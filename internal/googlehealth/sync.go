package googlehealth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robince/somascope/internal/providersync"
	"github.com/robince/somascope/internal/store"
)

const (
	dateLayout                = "2006-01-02"
	defaultBootstrapDays      = 30
	defaultIncrementalOverlap = 3
	defaultRetryAttempts      = 4
	defaultRequestWindow      = time.Minute
	defaultRequestBudget      = 240
)

type SyncOptions struct {
	StartDate string
	EndDate   string
	Tracker   *providersync.Tracker
}

func Sync(ctx context.Context, st *store.Store, client *Client, cfg AppConfig, connection store.Connection, options SyncOptions) error {
	if options.Tracker == nil {
		return fmt.Errorf("sync tracker is required")
	}
	tracker := options.Tracker

	activeConnection := connection
	if tokenExpired(activeConnection.TokenExpiresAt) && activeConnection.RefreshToken != "" {
		refreshed, err := client.RefreshToken(ctx, cfg, activeConnection.RefreshToken)
		if err != nil {
			_ = markNeedsReauth(ctx, st, activeConnection)
			return tracker.Fail("oauth", &store.SyncError{
				At:         nowRFC3339(),
				EntityKind: "oauth",
				Operation:  "refresh_token",
				Message:    fmt.Sprintf("refresh Google Health token: %v", err),
			})
		}
		if err := applyToken(ctx, st, &activeConnection, refreshed); err != nil {
			return tracker.Fail("oauth", &store.SyncError{
				At:         nowRFC3339(),
				EntityKind: "oauth",
				Operation:  "persist_refreshed_token",
				Message:    fmt.Sprintf("save refreshed Google Health token: %v", err),
			})
		}
	}

	endDate, err := resolveEndDate(options.EndDate)
	if err != nil {
		return err
	}
	startDate, err := resolveStartDate(ctx, st, options.StartDate, endDate)
	if err != nil {
		return err
	}
	if err := tracker.SetEffectiveRange(startDate.Format(dateLayout), endDate.Format(dateLayout)); err != nil {
		return err
	}

	syncClient := client
	if client != nil && client.RateLimiter == nil && (client.HTTPClient == nil || client.HTTPClient.Transport == nil) {
		syncClient = client.WithRateLimiter(NewRequestPacer(defaultRequestBudget, defaultRequestWindow))
	}

	var tokenMu sync.Mutex
	onUnauthorized := func(ctx context.Context, staleToken string) (string, error) {
		tokenMu.Lock()
		defer tokenMu.Unlock()
		if activeConnection.AccessToken != staleToken {
			return activeConnection.AccessToken, nil
		}
		if activeConnection.RefreshToken == "" {
			_ = markNeedsReauth(ctx, st, activeConnection)
			return "", fmt.Errorf("no refresh token available")
		}
		refreshed, err := client.RefreshToken(ctx, cfg, activeConnection.RefreshToken)
		if err != nil {
			_ = markNeedsReauth(ctx, st, activeConnection)
			return "", err
		}
		if err := applyToken(ctx, st, &activeConnection, refreshed); err != nil {
			return "", fmt.Errorf("save refreshed Google Health token: %w", err)
		}
		return activeConnection.AccessToken, nil
	}

	fetchedAt := time.Now().UTC().Format(time.RFC3339)
	retry := RetryConfig{
		MaxAttempts:    defaultRetryAttempts,
		OnUnauthorized: onUnauthorized,
	}

	if err := syncDailyActivity(ctx, st, syncClient, activeConnection.AccessToken, startDate, endDate, fetchedAt, tracker, retry); err != nil {
		return err
	}
	if err := syncSleep(ctx, st, syncClient, activeConnection.AccessToken, startDate, endDate, fetchedAt, tracker, retry); err != nil {
		return err
	}
	if err := syncSnapshots(ctx, st, syncClient, activeConnection.AccessToken, fetchedAt, tracker, retry); err != nil {
		return err
	}
	if err := syncRawArchives(ctx, st, syncClient, activeConnection.AccessToken, startDate, endDate, fetchedAt, tracker, retry); err != nil {
		return err
	}
	return nil
}

func syncDailyActivity(ctx context.Context, st *store.Store, client *Client, accessToken string, start, end time.Time, fetchedAt string, tracker *providersync.Tracker, retry RetryConfig) error {
	chunks := dateChunks(start, end, 14)
	if err := tracker.StartEntity("daily_activity", start.Format(dateLayout), end.Format(dateLayout), len(chunks)); err != nil {
		return err
	}

	for _, chunk := range chunks {
		if err := tracker.StartChunk("daily_activity", chunk.Start.Format(dateLayout), chunk.End.Format(dateLayout)); err != nil {
			return err
		}

		rowsWritten := 0
		byDate := map[string]map[string]any{}
		for _, dataType := range []string{"steps", "active-energy-burned", "total-calories", "distance", "active-minutes", "sedentary-period"} {
			page, err := client.DailyRollup(ctx, accessToken, dataType, chunk.Start, chunk.End, retryWith(retry, tracker, "daily_activity", chunk))
			if err != nil {
				return failEntity(tracker, "daily_activity", chunk, "dailyRollUp", err)
			}
			key := fmt.Sprintf("%s:dailyRollUp:%s:%s", dataType, chunk.Start.Format(dateLayout), chunk.End.Format(dateLayout))
			if _, err := archiveRaw(ctx, st, "daily_activity_"+strings.ReplaceAll(dataType, "-", "_"), key, chunk, page.RawBody, fetchedAt); err != nil {
				return err
			}
			for _, point := range page.Points {
				date := civilDateFrom(point)
				if date == "" {
					continue
				}
				if byDate[date] == nil {
					byDate[date] = map[string]any{"day": date}
				}
				mergeActivityPoint(byDate[date], dataType, point)
			}
		}

		for date, summary := range byDate {
			if date < chunk.Start.Format(dateLayout) || date > chunk.End.Format(dateLayout) {
				continue
			}
			if err := st.UpsertDailyRecord(ctx, store.DailyRecord{
				Provider:     Provider,
				RecordKind:   "daily_activity",
				LocalDate:    date,
				SourceDevice: stringValue(summary["source_device"]),
				ExternalID:   "daily_activity:" + date,
				Summary:      mustJSON(summary),
			}); err != nil {
				return err
			}
			rowsWritten++
		}

		if err := tracker.CompleteChunk("daily_activity", chunk.End.Format(dateLayout), rowsWritten); err != nil {
			return err
		}
	}

	_ = st.UpsertSyncState(ctx, Provider, "daily_activity", end.Format(dateLayout), fetchedAt)
	return tracker.CompleteEntity("daily_activity")
}

func syncSleep(ctx context.Context, st *store.Store, client *Client, accessToken string, start, end time.Time, fetchedAt string, tracker *providersync.Tracker, retry RetryConfig) error {
	if err := tracker.StartEntity("sleep", start.Format(dateLayout), end.Format(dateLayout), 1); err != nil {
		return err
	}
	chunk := dateChunk{Start: start, End: end}
	if err := tracker.StartChunk("sleep", start.Format(dateLayout), end.Format(dateLayout)); err != nil {
		return err
	}

	params := url.Values{}
	params.Set("dataSourceFamily", "users/me/dataSourceFamilies/google-wearables")
	params.Set("filter", civilDateRangeFilter("sleep.interval.civil_end_time", start, end))

	pages, err := client.ReconcileDataPoints(ctx, accessToken, "sleep", params, retryWith(retry, tracker, "sleep", chunk))
	if err != nil {
		return failEntity(tracker, "sleep", chunk, "reconcile", err)
	}

	rowsWritten := 0
	for i, page := range pages {
		if _, err := archiveRaw(ctx, st, "sleep", fmt.Sprintf("sleep:reconcile:%s:%d", start.Format(dateLayout), i), chunk, page.RawBody, fetchedAt); err != nil {
			return err
		}
		for _, item := range page.DataPoints {
			session, ok := sleepSessionFrom(item)
			if !ok {
				continue
			}
			if err := st.InsertSleepSession(ctx, session); err != nil {
				return err
			}
			if fallbackID := sleepFallbackExternalID(item); fallbackID != "" && fallbackID != session.ExternalID {
				_ = st.DeleteSleepSession(ctx, Provider, fallbackID)
			}
			rowsWritten++
		}
	}

	if err := tracker.CompleteChunk("sleep", end.Format(dateLayout), rowsWritten); err != nil {
		return err
	}
	_ = st.UpsertSyncState(ctx, Provider, "sleep", end.Format(dateLayout), fetchedAt)
	return tracker.CompleteEntity("sleep")
}

type rawQueryKind int

const (
	rawQueryDaily rawQueryKind = iota
	rawQueryInterval
	rawQuerySample
	rawQueryECG
	rawQueryNone
)

type rawEntity struct {
	kind     string
	dataType string
	query    rawQueryKind
	dense    bool
}

func rawArchiveEntities() []rawEntity {
	return []rawEntity{
		{kind: "daily_resting_heart_rate", dataType: "daily-resting-heart-rate", query: rawQueryDaily},
		{kind: "daily_heart_rate_variability", dataType: "daily-heart-rate-variability", query: rawQueryDaily},
		{kind: "daily_sleep_temperature_derivations", dataType: "daily-sleep-temperature-derivations", query: rawQueryDaily},
		{kind: "daily_oxygen_saturation", dataType: "daily-oxygen-saturation", query: rawQueryDaily},
		{kind: "daily_respiratory_rate", dataType: "daily-respiratory-rate", query: rawQueryDaily},
		{kind: "daily_heart_rate_zones", dataType: "daily-heart-rate-zones", query: rawQueryDaily},
		{kind: "daily_vo2_max", dataType: "daily-vo2-max", query: rawQueryDaily},
		{kind: "exercise", dataType: "exercise", query: rawQueryInterval},
		{kind: "heartrate", dataType: "heart-rate", query: rawQuerySample, dense: true},
		{kind: "heart_rate_variability", dataType: "heart-rate-variability", query: rawQuerySample, dense: true},
		{kind: "oxygen_saturation", dataType: "oxygen-saturation", query: rawQuerySample, dense: true},
		{kind: "steps_intraday", dataType: "steps", query: rawQueryInterval, dense: true},
		{kind: "respiratory_rate_sleep_summary", dataType: "respiratory-rate-sleep-summary", query: rawQuerySample},
		{kind: "core_body_temperature", dataType: "core-body-temperature", query: rawQuerySample},
		{kind: "blood_glucose", dataType: "blood-glucose", query: rawQuerySample},
		{kind: "body_fat", dataType: "body-fat", query: rawQuerySample},
		{kind: "height", dataType: "height", query: rawQuerySample},
		{kind: "weight", dataType: "weight", query: rawQuerySample},
		{kind: "run_vo2_max", dataType: "run-vo2-max", query: rawQuerySample},
		{kind: "activity_level", dataType: "activity-level", query: rawQueryInterval},
		{kind: "altitude", dataType: "altitude", query: rawQueryInterval},
		{kind: "active_zone_minutes", dataType: "active-zone-minutes", query: rawQueryInterval},
		{kind: "swim_lengths_data", dataType: "swim-lengths-data", query: rawQueryInterval},
		{kind: "nutrition_log", dataType: "nutrition-log", query: rawQuerySample},
		{kind: "hydration_log", dataType: "hydration-log", query: rawQueryInterval},
		{kind: "electrocardiogram", dataType: "electrocardiogram", query: rawQueryECG},
		{kind: "irregular_rhythm_notification", dataType: "irregular-rhythm-notification", query: rawQueryInterval},
	}
}

func syncSnapshots(ctx context.Context, st *store.Store, client *Client, accessToken, fetchedAt string, tracker *providersync.Tracker, retry RetryConfig) error {
	type snapshot struct {
		kind string
		path string
	}
	snapshots := []snapshot{
		{kind: "identity", path: "/users/me/identity"},
		{kind: "paired_devices", path: "/users/me/pairedDevices"},
	}
	if err := tracker.StartEntity("snapshots", "", "", len(snapshots)); err != nil {
		return err
	}
	chunk := dateChunk{}
	for _, item := range snapshots {
		if err := tracker.StartChunk("snapshots", item.kind, ""); err != nil {
			return err
		}
		raw, err := client.GetRaw(ctx, accessToken, item.path, retryWith(retry, tracker, "snapshots", chunk))
		if err != nil {
			if skipRawError(err) {
				log.Printf("warning: skipping google health %s: %v", item.kind, err)
				if completeErr := tracker.CompleteChunk("snapshots", item.kind, 0); completeErr != nil {
					return completeErr
				}
				continue
			}
			return failEntity(tracker, "snapshots", chunk, "get", err)
		}
		if _, err := archiveRaw(ctx, st, item.kind, item.kind, chunk, raw, fetchedAt); err != nil {
			return err
		}
		if err := tracker.CompleteChunk("snapshots", item.kind, 1); err != nil {
			return err
		}
	}
	_ = st.UpsertSyncState(ctx, Provider, "snapshots", fetchedAt, fetchedAt)
	return tracker.CompleteEntity("snapshots")
}

func syncRawArchives(ctx context.Context, st *store.Store, client *Client, accessToken string, start, end time.Time, fetchedAt string, tracker *providersync.Tracker, retry RetryConfig) error {
	for _, entity := range rawArchiveEntities() {
		chunks := dateChunks(start, end, 14)
		if entity.query == rawQueryECG {
			chunks = []dateChunk{{Start: start, End: end}}
		} else if entity.dense {
			chunks = dateChunks(start, end, 1)
		}
		if err := tracker.StartEntity(entity.kind, start.Format(dateLayout), end.Format(dateLayout), len(chunks)); err != nil {
			return err
		}

		skipped := false
		for _, chunk := range chunks {
			if err := tracker.StartChunk(entity.kind, chunk.Start.Format(dateLayout), chunk.End.Format(dateLayout)); err != nil {
				return err
			}
			params := url.Values{}
			if filter := rawFilter(entity, chunk.Start, chunk.End); filter != "" {
				params.Set("filter", filter)
			}
			pages, err := client.ListDataPoints(ctx, accessToken, entity.dataType, params, retryWith(retry, tracker, entity.kind, chunk))
			if err != nil {
				if skipRawError(err) {
					log.Printf("warning: skipping google health %s: %v", entity.kind, err)
					if completeErr := tracker.CompleteChunk(entity.kind, chunk.End.Format(dateLayout), 0); completeErr != nil {
						return completeErr
					}
					skipped = true
					break
				}
				return failEntity(tracker, entity.kind, chunk, "list", err)
			}
			rowsWritten := 0
			for i, page := range pages {
				if _, err := archiveRaw(ctx, st, entity.kind, fmt.Sprintf("%s:list:%s:%d", entity.dataType, chunk.Start.Format(dateLayout), i), chunk, page.RawBody, fetchedAt); err != nil {
					return err
				}
				rowsWritten += len(page.DataPoints)
			}
			if err := tracker.CompleteChunk(entity.kind, chunk.End.Format(dateLayout), rowsWritten); err != nil {
				return err
			}
		}
		if skipped {
			_ = tracker.CompleteEntity(entity.kind)
			continue
		}
		_ = st.UpsertSyncState(ctx, Provider, entity.kind, end.Format(dateLayout), fetchedAt)
		if err := tracker.CompleteEntity(entity.kind); err != nil {
			return err
		}
	}
	return nil
}

func rawFilter(entity rawEntity, start, end time.Time) string {
	field := kebabToSnake(entity.dataType)
	switch entity.query {
	case rawQueryDaily:
		return civilDateRangeFilter(field+".date", start, end)
	case rawQueryInterval:
		return civilDateRangeFilter(field+".interval.civil_start_time", start, end)
	case rawQuerySample:
		return physicalTimeRangeFilter(field+".sample_time.physical_time", start, end)
	case rawQueryECG:
		return fmt.Sprintf(`electrocardiogram.interval.start_time >= "%sT00:00:00Z"`, start.Format(dateLayout))
	default:
		return ""
	}
}

func skipRawError(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.StatusCode {
	case http.StatusForbidden, http.StatusNotFound:
		return true
	default:
		return false
	}
}

func mergeActivityPoint(summary map[string]any, dataType string, point map[string]any) {
	if device := deviceName(point); device != "" {
		summary["source_device"] = device
	}
	switch dataType {
	case "steps":
		if value := nestedInt(point, "steps", "countSum"); value != nil {
			summary["steps"] = *value
		}
	case "active-energy-burned":
		if value := nestedFloat(point, "activeEnergyBurned", "kcalSum"); value != nil {
			summary["active_calories"] = int(*value)
		}
	case "total-calories":
		if value := nestedFloat(point, "totalCalories", "kcalSum"); value != nil {
			summary["total_calories"] = int(*value)
		}
	case "distance":
		if value := distanceMeters(point); value != nil {
			summary["equivalent_walking_distance"] = *value
		}
	case "active-minutes":
		mergeActiveMinutes(summary, nestedMap(point, "activeMinutes"))
	case "sedentary-period":
		if value := sedentaryMinutes(point); value != nil {
			summary["resting_minutes"] = *value
		}
	}
}

func distanceMeters(point map[string]any) *int {
	if value := nestedFloat(point, "distance", "metersSum"); value != nil {
		meters := int(*value)
		return &meters
	}
	if value := nestedFloat(point, "distance", "millimetersSum"); value != nil {
		meters := int(*value / 1000)
		return &meters
	}
	return nil
}

func sedentaryMinutes(point map[string]any) *int {
	if value := nestedInt(point, "sedentaryPeriod", "minutesSum"); value != nil {
		return value
	}
	period := nestedMap(point, "sedentaryPeriod")
	if seconds := parseDurationSeconds(stringValue(period["durationSum"])); seconds > 0 {
		minutes := seconds / 60
		return &minutes
	}
	return nil
}

func mergeActiveMinutes(summary map[string]any, activeMinutes map[string]any) {
	if activeMinutes == nil {
		return
	}
	levels, _ := activeMinutes["activeMinutesRollupByActivityLevel"].([]any)
	if len(levels) == 0 {
		if value := intFromAny(activeMinutes["minutesSum"]); value != nil {
			summary["total_active_minutes"] = *value
		}
		return
	}
	highMinutes := 0
	for _, raw := range levels {
		level, _ := raw.(map[string]any)
		if level == nil {
			continue
		}
		minutes := intFromAny(level["activeMinutesSum"])
		if minutes == nil {
			continue
		}
		switch strings.ToUpper(stringValue(level["activityLevel"])) {
		case "LIGHT":
			summary["low_activity_minutes"] = *minutes
		case "MODERATE":
			summary["medium_activity_minutes"] = *minutes
		case "VIGOROUS", "PEAK":
			highMinutes += *minutes
		}
	}
	if highMinutes > 0 {
		summary["high_activity_minutes"] = highMinutes
	}
}

func parseDurationSeconds(value string) int {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return 0
	}
	if strings.HasSuffix(raw, "s") {
		raw = strings.TrimSuffix(raw, "s")
	}
	if parsed, err := strconv.Atoi(raw); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
		return int(parsed)
	}
	return 0
}

func sleepSessionFrom(item map[string]any) (store.SleepSession, bool) {
	sleep, _ := item["sleep"].(map[string]any)
	if sleep == nil {
		return store.SleepSession{}, false
	}
	interval, _ := sleep["interval"].(map[string]any)
	summary, _ := sleep["summary"].(map[string]any)
	metadata, _ := sleep["metadata"].(map[string]any)
	startTime := stringValue(interval["startTime"])
	endTime := stringValue(interval["endTime"])
	if startTime == "" || endTime == "" {
		return store.SleepSession{}, false
	}

	startOffset := firstNonEmpty(stringValue(interval["startUtcOffset"]), stringValue(interval["endUtcOffset"]))
	endOffset := firstNonEmpty(stringValue(interval["endUtcOffset"]), startOffset)
	localDate := civilDateString(nestedMap(interval, "civilEndTime"))
	if localDate == "" {
		localDate = localDateFromPhysical(endTime, endOffset)
	}

	duration := intFromAny(summary["minutesAsleep"])
	timeInBed := intFromAny(summary["minutesInSleepPeriod"])
	efficiency := efficiencyFrom(summary)
	mainSleep := isMainSleep(metadata)
	externalID := lastPathSegment(firstNonEmpty(stringValue(item["name"]), stringValue(item["dataPointName"])))
	if externalID == "" {
		externalID = startTime
	}

	return store.SleepSession{
		Provider:          Provider,
		LocalDate:         localDate,
		ZoneOffset:        zoneOffsetString(endOffset),
		ExternalID:        externalID,
		StartTime:         timestampWithOffset(startTime, startOffset),
		EndTime:           timestampWithOffset(endTime, endOffset),
		DurationMinutes:   duration,
		TimeInBedMinutes:  timeInBed,
		EfficiencyPercent: efficiency,
		IsNap:             !mainSleep,
		Stages:            mustJSON(stageMinutes(summary)),
		Metrics: mustJSON(map[string]any{
			"type":   firstNonEmpty(stringValue(sleep["type"]), stringValue(sleep["sleepType"])),
			"main":   mainSleep,
			"source": deviceName(item),
		}),
	}, true
}

func sleepFallbackExternalID(item map[string]any) string {
	sleep, _ := item["sleep"].(map[string]any)
	interval, _ := sleep["interval"].(map[string]any)
	return stringValue(interval["startTime"])
}

func isMainSleep(metadata map[string]any) bool {
	if metadata == nil {
		return false
	}
	if value, ok := metadata["mainSleep"]; ok {
		return boolValue(value)
	}
	return boolValue(metadata["main"])
}

func localDateFromPhysical(timestamp, utcOffset string) string {
	parsed, err := parseTimestamp(timestamp)
	if err != nil {
		return ""
	}
	return parsed.In(offsetLocation(utcOffset)).Format(dateLayout)
}

func timestampWithOffset(timestamp, utcOffset string) string {
	if strings.TrimSpace(utcOffset) == "" {
		return timestamp
	}
	parsed, err := parseTimestamp(timestamp)
	if err != nil {
		return timestamp
	}
	return parsed.In(offsetLocation(utcOffset)).Format("2006-01-02T15:04:05.000-07:00")
}

func zoneOffsetString(utcOffset string) string {
	if strings.TrimSpace(utcOffset) == "" {
		return ""
	}
	seconds := parseDurationSeconds(utcOffset)
	sign := "+"
	if seconds < 0 {
		sign = "-"
		seconds = -seconds
	}
	return fmt.Sprintf("%s%02d:%02d", sign, seconds/3600, (seconds%3600)/60)
}

func offsetLocation(utcOffset string) *time.Location {
	return time.FixedZone("offset", parseDurationSeconds(utcOffset))
}

func parseTimestamp(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.Parse(time.RFC3339, value)
}

func stageMinutes(summary map[string]any) map[string]any {
	out := map[string]any{}
	stages, _ := summary["stagesSummary"].([]any)
	for _, raw := range stages {
		stage, _ := raw.(map[string]any)
		if stage == nil {
			continue
		}
		minutes := intFromAny(stage["minutes"])
		if minutes == nil {
			continue
		}
		switch strings.ToUpper(stringValue(stage["type"])) {
		case "DEEP":
			out["deep_minutes"] = *minutes
		case "LIGHT":
			out["light_minutes"] = *minutes
		case "REM":
			out["rem_minutes"] = *minutes
		case "AWAKE":
			out["awake_minutes"] = *minutes
		}
	}
	return out
}

func efficiencyFrom(summary map[string]any) *float64 {
	asleep := intFromAny(summary["minutesAsleep"])
	inBed := intFromAny(summary["minutesInSleepPeriod"])
	if asleep == nil || inBed == nil || *inBed == 0 {
		return nil
	}
	value := float64(*asleep) / float64(*inBed) * 100
	return &value
}

type dateChunk struct {
	Start time.Time
	End   time.Time
}

func dateChunks(start, end time.Time, maxDays int) []dateChunk {
	if maxDays < 1 {
		maxDays = 1
	}
	var out []dateChunk
	cursor := start
	for !cursor.After(end) {
		chunkEnd := cursor.AddDate(0, 0, maxDays-1)
		if chunkEnd.After(end) {
			chunkEnd = end
		}
		out = append(out, dateChunk{Start: cursor, End: chunkEnd})
		cursor = chunkEnd.AddDate(0, 0, 1)
	}
	return out
}

func resolveStartDate(ctx context.Context, st *store.Store, requested string, end time.Time) (time.Time, error) {
	if strings.TrimSpace(requested) != "" {
		return time.Parse(dateLayout, requested)
	}
	cursor, _, err := st.SyncState(ctx, Provider, "daily_activity")
	if err == nil {
		if parsed, parseErr := time.Parse(dateLayout, cursor); parseErr == nil {
			return parsed.AddDate(0, 0, -defaultIncrementalOverlap), nil
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return time.Time{}, err
	}
	return end.AddDate(0, 0, -defaultBootstrapDays+1), nil
}

func resolveEndDate(requested string) (time.Time, error) {
	if strings.TrimSpace(requested) != "" {
		return time.Parse(dateLayout, requested)
	}
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
}

func archiveRaw(ctx context.Context, st *store.Store, kind, key string, chunk dateChunk, payload json.RawMessage, fetchedAt string) (int64, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	return st.UpsertRawDocument(ctx, store.RawDocument{
		Provider:     Provider,
		DocumentKind: kind,
		LocalDate:    formatChunkDate(chunk.Start),
		RequestPath:  key,
		RequestStart: formatChunkDate(chunk.Start),
		RequestEnd:   formatChunkDate(chunk.End),
		Payload:      payload,
		FetchedAt:    fetchedAt,
		DocumentKey:  key,
	})
}

func formatChunkDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(dateLayout)
}

func retryWith(retry RetryConfig, tracker *providersync.Tracker, entity string, chunk dateChunk) RetryConfig {
	retry.OnRetry = func(apiErr *APIError, backoff time.Duration) {
		_ = tracker.Retry(entity, &store.SyncError{
			At:             nowRFC3339(),
			EntityKind:     entity,
			ChunkStartDate: chunk.Start.Format(dateLayout),
			ChunkEndDate:   chunk.End.Format(dateLayout),
			Operation:      apiErr.Method,
			Endpoint:       apiErr.Path,
			HTTPStatus:     apiErr.StatusCode,
			Attempt:        apiErr.Attempt,
			Retriable:      true,
			Message:        apiErr.Error(),
			ResponseBody:   apiErr.ResponseBody,
		}, backoff)
	}
	return retry
}

func civilDateRangeFilter(field string, start, end time.Time) string {
	return fmt.Sprintf(`%s >= "%s" AND %s < "%s"`, field, start.Format(dateLayout), field, end.AddDate(0, 0, 1).Format(dateLayout))
}

func physicalTimeRangeFilter(field string, start, end time.Time) string {
	return fmt.Sprintf(`%s >= "%sT00:00:00Z" AND %s < "%sT00:00:00Z"`, field, start.Format(dateLayout), field, end.AddDate(0, 0, 1).Format(dateLayout))
}

func kebabToSnake(value string) string {
	return strings.ReplaceAll(value, "-", "_")
}

func failEntity(tracker *providersync.Tracker, entity string, chunk dateChunk, operation string, err error) error {
	apiErr, _ := err.(*APIError)
	syncErr := &store.SyncError{
		At:             nowRFC3339(),
		EntityKind:     entity,
		ChunkStartDate: chunk.Start.Format(dateLayout),
		ChunkEndDate:   chunk.End.Format(dateLayout),
		Operation:      operation,
		Message:        err.Error(),
	}
	if apiErr != nil {
		syncErr.Endpoint = apiErr.Path
		syncErr.HTTPStatus = apiErr.StatusCode
		syncErr.Attempt = apiErr.Attempt
		syncErr.ResponseBody = apiErr.ResponseBody
	}
	_ = tracker.Fail(entity, syncErr)
	return err
}

func applyToken(ctx context.Context, st *store.Store, connection *store.Connection, bundle TokenBundle) error {
	connection.AccessToken = bundle.AccessToken
	if bundle.RefreshToken != "" {
		connection.RefreshToken = bundle.RefreshToken
	}
	connection.Scope = firstNonEmpty(bundle.Scope, connection.Scope)
	if !bundle.ExpiresAt.IsZero() {
		connection.TokenExpiresAt = bundle.ExpiresAt.UTC().Format(time.RFC3339)
	}
	connection.Status = "connected"
	return st.UpsertConnection(ctx, *connection)
}

func markNeedsReauth(ctx context.Context, st *store.Store, connection store.Connection) error {
	connection.Status = "needs_reauth"
	return st.UpsertConnection(ctx, connection)
}

func tokenExpired(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return !parsed.After(time.Now().UTC().Add(2 * time.Minute))
}

func civilDateFrom(point map[string]any) string {
	if date := civilDateString(nestedMap(point, "civilStartTime")); date != "" {
		return date
	}
	return civilDateString(nestedMap(point, "civilEndTime"))
}

func civilDateString(value map[string]any) string {
	date := nestedMap(value, "date")
	if date == nil {
		return ""
	}
	year := intFromAny(date["year"])
	month := intFromAny(date["month"])
	day := intFromAny(date["day"])
	if year == nil || month == nil || day == nil {
		return ""
	}
	return time.Date(*year, time.Month(*month), *day, 0, 0, 0, 0, time.UTC).Format(dateLayout)
}

func deviceName(item map[string]any) string {
	source, _ := item["dataSource"].(map[string]any)
	device, _ := source["device"].(map[string]any)
	return firstNonEmpty(stringValue(device["displayName"]), stringValue(device["deviceVersion"]))
}

func nestedMap(values map[string]any, key string) map[string]any {
	if values == nil {
		return nil
	}
	child, _ := values[key].(map[string]any)
	return child
}

func nestedInt(values map[string]any, keys ...string) *int {
	current := any(values)
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	return intFromAny(current)
}

func nestedFloat(values map[string]any, keys ...string) *float64 {
	current := any(values)
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	return floatFromAny(current)
}

func intFromAny(value any) *int {
	switch typed := value.(type) {
	case int:
		copy := typed
		return &copy
	case int64:
		copy := int(typed)
		return &copy
	case float64:
		copy := int(typed)
		return &copy
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return nil
		}
		copy := int(parsed)
		return &copy
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return nil
		}
		return &parsed
	default:
		return nil
	}
}

func floatFromAny(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		copy := typed
		return &copy
	case float32:
		copy := float64(typed)
		return &copy
	case int:
		copy := float64(typed)
		return &copy
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return nil
		}
		return &parsed
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return nil
		}
		return &parsed
	default:
		return nil
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func boolValue(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func lastPathSegment(value string) string {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

func mustJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
