package oura

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/robince/somascope/internal/providersync"
	"github.com/robince/somascope/internal/store"
)

const (
	dateLayout                = "2006-01-02"
	defaultBootstrapDays      = 30
	defaultIncrementalOverlap = 3
	defaultRetryAttempts      = 4
)

type SyncOptions struct {
	StartDate string
	EndDate   string
	Tracker   *providersync.Tracker
}

type queryMode string

const (
	queryModeNone     queryMode = "none"
	queryModeDate     queryMode = "date"
	queryModeDateTime queryMode = "datetime"
)

type syncEntity struct {
	kind        string
	featurePath string
	chunkDays   int
	queryMode   queryMode
	useCursor   bool
	apply       func(context.Context, *store.Store, map[string]any, string) error
}

func Sync(ctx context.Context, st *store.Store, client *Client, cfg AppConfig, connection store.Connection, options SyncOptions) error {
	tracker := options.Tracker
	if tracker == nil {
		return fmt.Errorf("sync tracker is required")
	}

	activeConnection := connection
	if tokenExpired(activeConnection.TokenExpiresAt) && activeConnection.RefreshToken != "" {
		refreshed, err := client.RefreshToken(ctx, cfg, activeConnection.RefreshToken)
		if err != nil {
			return tracker.Fail("oauth", &store.SyncError{
				At:         now(),
				EntityKind: "oauth",
				Operation:  "refresh_token",
				Message:    fmt.Sprintf("refresh Oura token: %v", err),
			})
		}
		activeConnection.AccessToken = refreshed.AccessToken
		if refreshed.RefreshToken != "" {
			activeConnection.RefreshToken = refreshed.RefreshToken
		}
		activeConnection.Scope = firstNonEmpty(refreshed.Scope, activeConnection.Scope)
		activeConnection.TokenExpiresAt = isoTime(refreshed.ExpiresAt)
		activeConnection.Status = "connected"
		if err := st.UpsertConnection(ctx, activeConnection); err != nil {
			return err
		}
	}

	endDate, err := resolveEndDate(options.EndDate)
	if err != nil {
		return err
	}
	fetchedAt := time.Now().UTC().Format(time.RFC3339)
	entities := syncEntities()

	overallStart := endDate
	for _, entity := range entities {
		if entity.queryMode == queryModeNone {
			continue
		}
		entityStart, err := resolveEntityStart(ctx, st, entity.kind, options.StartDate, endDate, entity.useCursor)
		if err != nil {
			return err
		}
		if entityStart.Before(overallStart) {
			overallStart = entityStart
		}
	}
	if err := tracker.SetEffectiveRange(overallStart.Format(dateLayout), endDate.Format(dateLayout)); err != nil {
		return err
	}

	for _, entity := range entities {
		switch entity.queryMode {
		case queryModeNone:
			if err := syncEntityNoRange(ctx, st, client, activeConnection.AccessToken, fetchedAt, entity, tracker); err != nil {
				return err
			}
		default:
			entityStart, err := resolveEntityStart(ctx, st, entity.kind, options.StartDate, endDate, entity.useCursor)
			if err != nil {
				return err
			}
			if err := syncEntityRange(ctx, st, client, activeConnection.AccessToken, fetchedAt, entity, entityStart, endDate, tracker); err != nil {
				return err
			}
		}
	}

	return nil
}

func syncEntities() []syncEntity {
	return []syncEntity{
		{kind: "personal_info", featurePath: "/v2/usercollection/personal_info", queryMode: queryModeNone, apply: applyRawOnly("personal_info")},
		{kind: "tag", featurePath: "/v2/usercollection/tag", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("tag")},
		{kind: "enhanced_tag", featurePath: "/v2/usercollection/enhanced_tag", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("enhanced_tag")},
		{kind: "workout", featurePath: "/v2/usercollection/workout", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("workout")},
		{kind: "session", featurePath: "/v2/usercollection/session", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("session")},
		{kind: "daily_activity", featurePath: "/v2/usercollection/daily_activity", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyDailyActivity},
		{kind: "daily_sleep", featurePath: "/v2/usercollection/daily_sleep", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("daily_sleep")},
		{kind: "daily_spo2", featurePath: "/v2/usercollection/daily_spo2", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("daily_spo2")},
		{kind: "daily_readiness", featurePath: "/v2/usercollection/daily_readiness", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyDailyReadiness},
		{kind: "sleep", featurePath: "/v2/usercollection/sleep", chunkDays: 30, queryMode: queryModeDate, useCursor: true, apply: applySleep},
		{kind: "sleep_time", featurePath: "/v2/usercollection/sleep_time", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("sleep_time")},
		{kind: "rest_mode_period", featurePath: "/v2/usercollection/rest_mode_period", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("rest_mode_period")},
		{kind: "daily_stress", featurePath: "/v2/usercollection/daily_stress", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("daily_stress")},
		{kind: "daily_resilience", featurePath: "/v2/usercollection/daily_resilience", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("daily_resilience")},
		{kind: "daily_cardiovascular_age", featurePath: "/v2/usercollection/daily_cardiovascular_age", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("daily_cardiovascular_age")},
		{kind: "vo2_max", featurePath: "/v2/usercollection/vO2_max", chunkDays: 90, queryMode: queryModeDate, useCursor: true, apply: applyRawOnly("vo2_max")},
		{kind: "heartrate", featurePath: "/v2/usercollection/heartrate", chunkDays: 14, queryMode: queryModeDateTime, useCursor: true, apply: applyRawOnly("heartrate")},
	}
}

func syncEntityNoRange(ctx context.Context, st *store.Store, client *Client, accessToken, fetchedAt string, entity syncEntity, tracker *providersync.Tracker) error {
	if err := tracker.StartEntity(entity.kind, "", "", 1); err != nil {
		return err
	}
	if err := tracker.StartChunk(entity.kind, "", ""); err != nil {
		return err
	}

	var (
		items []map[string]any
		err   error
	)
	switch entity.kind {
	case "personal_info":
		var item map[string]any
		item, err = client.FetchDocument(ctx, accessToken, entity.featurePath, RetryConfig{
			MaxAttempts: defaultRetryAttempts,
			OnRetry:     retryCallback(tracker, entity, "", ""),
		})
		if err == nil && len(item) > 0 {
			items = []map[string]any{item}
		}
	default:
		items, err = client.FetchCollection(ctx, accessToken, entity.featurePath, nil, RetryConfig{
			MaxAttempts: defaultRetryAttempts,
			OnRetry:     retryCallback(tracker, entity, "", ""),
		})
	}
	if err != nil {
		return failEntity(tracker, entity, "", "", "fetch_collection", err)
	}

	for _, item := range items {
		if err := entity.apply(ctx, st, item, fetchedAt); err != nil {
			return failEntity(tracker, entity, "", "", "apply_document", err)
		}
	}
	if err := tracker.CompleteChunk(entity.kind, "", len(items)); err != nil {
		return err
	}
	return tracker.CompleteEntity(entity.kind)
}

func syncEntityRange(ctx context.Context, st *store.Store, client *Client, accessToken, fetchedAt string, entity syncEntity, startDate, endDate time.Time, tracker *providersync.Tracker) error {
	if startDate.After(endDate) {
		startDate = endDate
	}
	totalChunks := countChunks(startDate, endDate, entity.chunkDays)
	if err := tracker.StartEntity(entity.kind, startDate.Format(dateLayout), endDate.Format(dateLayout), totalChunks); err != nil {
		return err
	}

	for chunkStart := startDate; !chunkStart.After(endDate); chunkStart = chunkStart.AddDate(0, 0, entity.chunkDays) {
		chunkEnd := minDate(chunkStart.AddDate(0, 0, entity.chunkDays-1), endDate)
		chunkStartLabel := chunkStart.Format(dateLayout)
		chunkEndLabel := chunkEnd.Format(dateLayout)
		if err := tracker.StartChunk(entity.kind, chunkStartLabel, chunkEndLabel); err != nil {
			return err
		}

		params := rangeParams(entity.queryMode, chunkStart, chunkEnd)
		items, err := client.FetchCollection(ctx, accessToken, entity.featurePath, params, RetryConfig{
			MaxAttempts: defaultRetryAttempts,
			OnRetry:     retryCallback(tracker, entity, chunkStartLabel, chunkEndLabel),
		})
		if err != nil {
			return failEntity(tracker, entity, chunkStartLabel, chunkEndLabel, "fetch_collection", err)
		}

		for _, item := range items {
			if err := entity.apply(ctx, st, item, fetchedAt); err != nil {
				return failEntity(tracker, entity, chunkStartLabel, chunkEndLabel, "apply_document", err)
			}
		}

		cursor := ""
		if entity.useCursor {
			cursor = chunkEndLabel
			if err := st.UpsertSyncState(ctx, "oura", entity.kind, cursor, fetchedAt); err != nil {
				return failEntity(tracker, entity, chunkStartLabel, chunkEndLabel, "update_sync_state", err)
			}
		}
		if err := tracker.CompleteChunk(entity.kind, cursor, len(items)); err != nil {
			return err
		}
	}

	return tracker.CompleteEntity(entity.kind)
}

func retryCallback(tracker *providersync.Tracker, entity syncEntity, chunkStartDate, chunkEndDate string) func(*APIError, time.Duration) {
	return func(apiErr *APIError, backoff time.Duration) {
		_ = tracker.Retry(entity.kind, &store.SyncError{
			At:             now(),
			EntityKind:     entity.kind,
			ChunkStartDate: chunkStartDate,
			ChunkEndDate:   chunkEndDate,
			Operation:      "fetch_collection",
			Endpoint:       entity.featurePath,
			HTTPStatus:     apiErr.StatusCode,
			Attempt:        apiErr.Attempt,
			Retriable:      true,
			Message:        apiErr.Error(),
			ResponseBody:   apiErr.ResponseBody,
		}, backoff)
	}
}

func failEntity(tracker *providersync.Tracker, entity syncEntity, chunkStartDate, chunkEndDate, operation string, err error) error {
	syncErr := toSyncError(entity.kind, entity.featurePath, chunkStartDate, chunkEndDate, operation, err)
	if tracker != nil {
		_ = tracker.Fail(entity.kind, syncErr)
	}
	return fmt.Errorf("sync oura %s %s..%s: %w", entity.kind, chunkStartDate, chunkEndDate, err)
}

func toSyncError(entityKind, endpoint, chunkStartDate, chunkEndDate, operation string, err error) *store.SyncError {
	out := &store.SyncError{
		At:             now(),
		EntityKind:     entityKind,
		ChunkStartDate: chunkStartDate,
		ChunkEndDate:   chunkEndDate,
		Operation:      operation,
		Endpoint:       endpoint,
		Message:        err.Error(),
	}
	var apiErr *APIError
	if errorsAsAPI(err, &apiErr) {
		out.HTTPStatus = apiErr.StatusCode
		out.Attempt = apiErr.Attempt
		out.Retriable = apiErr.Retriable()
		out.ResponseBody = apiErr.ResponseBody
	}
	return out
}

func rangeParams(mode queryMode, startDate, endDate time.Time) url.Values {
	params := url.Values{}
	switch mode {
	case queryModeDate:
		params.Set("start_date", startDate.Format(dateLayout))
		params.Set("end_date", endDate.Format(dateLayout))
	case queryModeDateTime:
		params.Set("start_datetime", startDate.UTC().Format(time.RFC3339))
		params.Set("end_datetime", time.Date(endDate.Year(), endDate.Month(), endDate.Day(), 23, 59, 59, 0, time.UTC).Format(time.RFC3339))
	}
	return params
}

func countChunks(startDate, endDate time.Time, chunkDays int) int {
	if chunkDays < 1 || startDate.After(endDate) {
		return 0
	}
	count := 0
	for chunkStart := startDate; !chunkStart.After(endDate); chunkStart = chunkStart.AddDate(0, 0, chunkDays) {
		count++
	}
	return count
}

func resolveEntityStart(ctx context.Context, st *store.Store, entityKind, explicitStart string, endDate time.Time, useCursor bool) (time.Time, error) {
	if strings.TrimSpace(explicitStart) != "" {
		startDate, err := parseDate(explicitStart)
		if err != nil {
			return time.Time{}, err
		}
		if startDate.After(endDate) {
			startDate = endDate
		}
		return startDate, nil
	}

	if !useCursor {
		startDate := endDate.AddDate(0, 0, -(defaultBootstrapDays - 1))
		return startDate, nil
	}

	cursorValue, _, err := st.SyncState(ctx, "oura", entityKind)
	switch {
	case err == nil && strings.TrimSpace(cursorValue) != "":
		cursorDate, parseErr := parseDate(cursorValue)
		if parseErr != nil {
			return time.Time{}, parseErr
		}
		startDate := cursorDate.AddDate(0, 0, -defaultIncrementalOverlap)
		if startDate.After(endDate) {
			startDate = endDate
		}
		return startDate, nil
	case err == nil || err == store.ErrNotFound:
		startDate := endDate.AddDate(0, 0, -(defaultBootstrapDays - 1))
		return startDate, nil
	default:
		return time.Time{}, err
	}
}

func resolveEndDate(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		now := time.Now().UTC()
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	return parseDate(value)
}

func parseDate(value string) (time.Time, error) {
	parsed, err := time.Parse(dateLayout, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q: expected YYYY-MM-DD", value)
	}
	return parsed.UTC(), nil
}

func applyRawOnly(documentKind string) func(context.Context, *store.Store, map[string]any, string) error {
	return func(ctx context.Context, st *store.Store, item map[string]any, fetchedAt string) error {
		_, err := st.UpsertRawDocument(ctx, store.RawDocument{
			Provider:     "oura",
			DocumentKind: documentKind,
			ExternalID:   rawExternalID(item),
			LocalDate:    rawLocalDate(item),
			ZoneOffset:   rawZoneOffset(item),
			Payload:      mustJSON(item),
			FetchedAt:    fetchedAt,
			DocumentKey:  rawDocumentKey(documentKind, item),
		})
		return err
	}
}

func applyDailyActivity(ctx context.Context, st *store.Store, item map[string]any, fetchedAt string) error {
	rawID, err := st.UpsertRawDocument(ctx, store.RawDocument{
		Provider:     "oura",
		DocumentKind: "daily_activity",
		ExternalID:   stringValue(item["id"]),
		LocalDate:    stringValue(item["day"]),
		Payload:      mustJSON(item),
		FetchedAt:    fetchedAt,
		DocumentKey:  rawDocumentKey("daily_activity", item),
	})
	if err != nil {
		return err
	}
	return st.UpsertDailyRecord(ctx, store.DailyRecord{
		Provider:      "oura",
		RecordKind:    "daily_activity",
		LocalDate:     stringValue(item["day"]),
		ZoneOffset:    zoneOffset(item["timestamp"]),
		SourceDevice:  "oura-ring",
		ExternalID:    stringValue(item["id"]),
		Summary:       normalizeDailyActivity(item),
		RawDocumentID: &rawID,
	})
}

func applyDailyReadiness(ctx context.Context, st *store.Store, item map[string]any, fetchedAt string) error {
	rawID, err := st.UpsertRawDocument(ctx, store.RawDocument{
		Provider:     "oura",
		DocumentKind: "daily_readiness",
		ExternalID:   stringValue(item["id"]),
		LocalDate:    stringValue(item["day"]),
		Payload:      mustJSON(item),
		FetchedAt:    fetchedAt,
		DocumentKey:  rawDocumentKey("daily_readiness", item),
	})
	if err != nil {
		return err
	}
	return st.UpsertDailyRecord(ctx, store.DailyRecord{
		Provider:      "oura",
		RecordKind:    "daily_readiness",
		LocalDate:     stringValue(item["day"]),
		ZoneOffset:    zoneOffset(item["timestamp"]),
		SourceDevice:  "oura-ring",
		ExternalID:    stringValue(item["id"]),
		Summary:       normalizeDailyReadiness(item),
		RawDocumentID: &rawID,
	})
}

func applySleep(ctx context.Context, st *store.Store, item map[string]any, fetchedAt string) error {
	rawID, err := st.UpsertRawDocument(ctx, store.RawDocument{
		Provider:     "oura",
		DocumentKind: "sleep",
		ExternalID:   stringValue(item["id"]),
		LocalDate:    stringValue(item["day"]),
		Payload:      mustJSON(item),
		FetchedAt:    fetchedAt,
		DocumentKey:  rawDocumentKey("sleep", item),
	})
	if err != nil {
		return err
	}

	durationMinutes := secondsToMinutes(item["total_sleep_duration"])
	timeInBedMinutes := secondsToMinutes(item["time_in_bed"])
	efficiency := floatPointer(item["efficiency"])

	return st.InsertSleepSession(ctx, store.SleepSession{
		Provider:          "oura",
		LocalDate:         stringValue(item["day"]),
		ZoneOffset:        zoneOffset(item["bedtime_start"]),
		ExternalID:        stringValue(item["id"]),
		StartTime:         stringValue(item["bedtime_start"]),
		EndTime:           stringValue(item["bedtime_end"]),
		DurationMinutes:   durationMinutes,
		TimeInBedMinutes:  timeInBedMinutes,
		EfficiencyPercent: efficiency,
		IsNap:             strings.EqualFold(stringValue(item["type"]), "rest"),
		Stages:            normalizeSleepStages(item),
		Metrics:           normalizeSleepMetrics(item),
		RawDocumentID:     &rawID,
	})
}

func normalizeDailyActivity(item map[string]any) json.RawMessage {
	return marshalMap(filteredMap(item, []string{
		"day", "score", "steps", "active_calories", "total_calories", "equivalent_walking_distance",
		"high_activity_time", "medium_activity_time", "low_activity_time", "resting_time",
		"inactivity_alerts", "average_met_minutes", "non_wear_time", "contributors",
	}))
}

func normalizeDailyReadiness(item map[string]any) json.RawMessage {
	return marshalMap(filteredMap(item, []string{
		"day", "score", "temperature_deviation", "temperature_trend_deviation", "contributors", "timestamp",
	}))
}

func normalizeSleepStages(item map[string]any) json.RawMessage {
	return marshalMap(filteredMap(item, []string{
		"deep_sleep_duration", "light_sleep_duration", "rem_sleep_duration", "awake_time",
	}))
}

func normalizeSleepMetrics(item map[string]any) json.RawMessage {
	return marshalMap(filteredMap(item, []string{
		"average_breath", "average_heart_rate", "average_hrv", "efficiency", "latency",
		"lowest_heart_rate", "restless_periods", "time_in_bed", "total_sleep_duration", "type",
	}))
}

func tokenExpired(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return time.Now().UTC().After(parsed.Add(-2 * time.Minute))
}

func mustJSON(value map[string]any) json.RawMessage {
	return marshalMap(value)
}

func marshalMap(value map[string]any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(data)
}

func filteredMap(item map[string]any, keys []string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if value, ok := item[key]; ok && value != nil {
			out[key] = value
		}
	}
	return out
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", value)
	}
}

func zoneOffset(value any) string {
	raw := strings.TrimSpace(stringValue(value))
	if len(raw) >= 6 {
		offset := raw[len(raw)-6:]
		if strings.HasPrefix(offset, "+") || strings.HasPrefix(offset, "-") {
			return offset
		}
	}
	return ""
}

func secondsToMinutes(value any) *int {
	number, ok := floatFromAny(value)
	if !ok {
		return nil
	}
	minutes := int(number / 60)
	return &minutes
}

func floatPointer(value any) *float64 {
	number, ok := floatFromAny(value)
	if !ok {
		return nil
	}
	return &number
}

func floatFromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}

func rawExternalID(item map[string]any) string {
	for _, key := range []string{"id", "document_id"} {
		if value := strings.TrimSpace(stringValue(item[key])); value != "" {
			return value
		}
	}
	return ""
}

func rawLocalDate(item map[string]any) string {
	for _, key := range []string{"day", "date"} {
		if value := strings.TrimSpace(stringValue(item[key])); value != "" {
			return value
		}
	}
	for _, key := range []string{"timestamp", "start_time", "start_datetime", "bedtime_start"} {
		if value := strings.TrimSpace(stringValue(item[key])); len(value) >= 10 {
			return value[:10]
		}
	}
	return ""
}

func rawZoneOffset(item map[string]any) string {
	for _, key := range []string{"timestamp", "start_time", "start_datetime", "bedtime_start", "period_start"} {
		if offset := zoneOffset(item[key]); offset != "" {
			return offset
		}
	}
	return ""
}

func rawDocumentKey(documentKind string, item map[string]any) string {
	if externalID := rawExternalID(item); externalID != "" {
		return "id:" + externalID
	}

	parts := []string{
		strings.TrimSpace(stringValue(item["day"])),
		strings.TrimSpace(stringValue(item["date"])),
		strings.TrimSpace(stringValue(item["timestamp"])),
		strings.TrimSpace(stringValue(item["start_time"])),
		strings.TrimSpace(stringValue(item["start_datetime"])),
		strings.TrimSpace(stringValue(item["bedtime_start"])),
		strings.TrimSpace(stringValue(item["bedtime_end"])),
		strings.TrimSpace(stringValue(item["period_start"])),
		strings.TrimSpace(stringValue(item["period_end"])),
	}
	parts = slices.DeleteFunc(parts, func(value string) bool { return value == "" })
	if len(parts) > 0 {
		return documentKind + ":" + strings.Join(parts, "|")
	}

	hash := sha1.Sum(mustJSON(item))
	return documentKind + ":sha1:" + hex.EncodeToString(hash[:])
}

func firstNonEmpty(values ...string) string {
	index := slices.IndexFunc(values, func(value string) bool { return strings.TrimSpace(value) != "" })
	if index < 0 {
		return ""
	}
	return values[index]
}

func minDate(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func isoTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func errorsAsAPI(err error, target **APIError) bool {
	apiErr, ok := err.(*APIError)
	if !ok {
		return false
	}
	*target = apiErr
	return true
}
