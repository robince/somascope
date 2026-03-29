package oura

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/robince/somascope/internal/store"
)

const (
	dateLayout                = "2006-01-02"
	defaultBootstrapDays      = 30
	defaultIncrementalOverlap = 3
)

type SyncOptions struct {
	StartDate string
	EndDate   string
}

type EntitySyncResult struct {
	Entity     string `json:"entity"`
	StartDate  string `json:"start_date"`
	EndDate    string `json:"end_date"`
	Cursor     string `json:"cursor"`
	Rows       int    `json:"rows"`
	ChunkCount int    `json:"chunk_count"`
}

type SyncResult struct {
	FetchedAt          string             `json:"fetched_at"`
	Mode               string             `json:"mode"`
	StartDate          string             `json:"start_date"`
	EndDate            string             `json:"end_date"`
	DailyActivityRows  int                `json:"daily_activity_rows"`
	DailyReadinessRows int                `json:"daily_readiness_rows"`
	SleepRows          int                `json:"sleep_rows"`
	Entities           []EntitySyncResult `json:"entities"`
}

type syncEntity struct {
	kind      string
	path      string
	chunkDays int
	apply     func(context.Context, *store.Store, map[string]any, string) error
}

func Sync(ctx context.Context, st *store.Store, client *Client, cfg AppConfig, connection store.Connection, options SyncOptions) (SyncResult, store.Connection, error) {
	activeConnection := connection
	if tokenExpired(activeConnection.TokenExpiresAt) && activeConnection.RefreshToken != "" {
		refreshed, err := client.RefreshToken(ctx, cfg, activeConnection.RefreshToken)
		if err != nil {
			return SyncResult{}, activeConnection, fmt.Errorf("refresh Oura token: %w", err)
		}
		activeConnection.AccessToken = refreshed.AccessToken
		if refreshed.RefreshToken != "" {
			activeConnection.RefreshToken = refreshed.RefreshToken
		}
		activeConnection.Scope = firstNonEmpty(refreshed.Scope, activeConnection.Scope)
		activeConnection.TokenExpiresAt = isoTime(refreshed.ExpiresAt)
		activeConnection.Status = "connected"
		if err := st.UpsertConnection(ctx, activeConnection); err != nil {
			return SyncResult{}, activeConnection, err
		}
	}

	endDate, err := resolveEndDate(options.EndDate)
	if err != nil {
		return SyncResult{}, activeConnection, err
	}

	fetchedAt := time.Now().UTC().Format(time.RFC3339)
	result := SyncResult{
		FetchedAt: fetchedAt,
		EndDate:   endDate.Format(dateLayout),
		Mode:      "incremental",
	}

	entities := []syncEntity{
		{
			kind:      "daily_activity",
			path:      "/v2/usercollection/daily_activity",
			chunkDays: 90,
			apply:     applyDailyActivity,
		},
		{
			kind:      "daily_readiness",
			path:      "/v2/usercollection/daily_readiness",
			chunkDays: 90,
			apply:     applyDailyReadiness,
		},
		{
			kind:      "sleep",
			path:      "/v2/usercollection/sleep",
			chunkDays: 30,
			apply:     applySleep,
		},
	}

	var overallStart time.Time
	for i, entity := range entities {
		entityStart, mode, err := resolveEntityStart(ctx, st, entity.kind, options.StartDate, endDate)
		if err != nil {
			return SyncResult{}, activeConnection, err
		}
		if i == 0 || entityStart.Before(overallStart) {
			overallStart = entityStart
		}
		if mode == "backfill" {
			result.Mode = "backfill"
		}

		entityResult, err := syncEntityRange(ctx, st, client, activeConnection.AccessToken, fetchedAt, entity, entityStart, endDate)
		if err != nil {
			return SyncResult{}, activeConnection, err
		}
		result.Entities = append(result.Entities, entityResult)
		switch entity.kind {
		case "daily_activity":
			result.DailyActivityRows += entityResult.Rows
		case "daily_readiness":
			result.DailyReadinessRows += entityResult.Rows
		case "sleep":
			result.SleepRows += entityResult.Rows
		}
	}

	result.StartDate = overallStart.Format(dateLayout)
	return result, activeConnection, nil
}

func syncEntityRange(ctx context.Context, st *store.Store, client *Client, accessToken, fetchedAt string, entity syncEntity, startDate, endDate time.Time) (EntitySyncResult, error) {
	if startDate.After(endDate) {
		startDate = endDate
	}

	out := EntitySyncResult{
		Entity:    entity.kind,
		StartDate: startDate.Format(dateLayout),
		EndDate:   endDate.Format(dateLayout),
		Cursor:    startDate.Format(dateLayout),
	}

	for chunkStart := startDate; !chunkStart.After(endDate); chunkStart = chunkStart.AddDate(0, 0, entity.chunkDays) {
		chunkEnd := minDate(chunkStart.AddDate(0, 0, entity.chunkDays-1), endDate)
		items, err := client.FetchCollection(ctx, accessToken, entity.path, chunkStart.Format(dateLayout), chunkEnd.Format(dateLayout))
		if err != nil {
			return EntitySyncResult{}, fmt.Errorf("sync oura %s %s..%s: %w", entity.kind, chunkStart.Format(dateLayout), chunkEnd.Format(dateLayout), err)
		}
		for _, item := range items {
			if err := entity.apply(ctx, st, item, fetchedAt); err != nil {
				return EntitySyncResult{}, err
			}
			out.Rows++
		}
		out.ChunkCount++
		out.Cursor = chunkEnd.Format(dateLayout)
		if err := st.UpsertSyncState(ctx, "oura", entity.kind, out.Cursor, fetchedAt); err != nil {
			return EntitySyncResult{}, err
		}
	}

	return out, nil
}

func resolveEntityStart(ctx context.Context, st *store.Store, entityKind, explicitStart string, endDate time.Time) (time.Time, string, error) {
	if strings.TrimSpace(explicitStart) != "" {
		startDate, err := parseDate(explicitStart)
		if err != nil {
			return time.Time{}, "", err
		}
		if startDate.After(endDate) {
			startDate = endDate
		}
		return startDate, "backfill", nil
	}

	cursorValue, _, err := st.SyncState(ctx, "oura", entityKind)
	switch {
	case err == nil && strings.TrimSpace(cursorValue) != "":
		cursorDate, parseErr := parseDate(cursorValue)
		if parseErr != nil {
			return time.Time{}, "", parseErr
		}
		startDate := cursorDate.AddDate(0, 0, -defaultIncrementalOverlap)
		if startDate.After(endDate) {
			startDate = endDate
		}
		return startDate, "incremental", nil
	case err == nil || err == store.ErrNotFound:
		startDate := endDate.AddDate(0, 0, -(defaultBootstrapDays - 1))
		return startDate, "incremental", nil
	default:
		return time.Time{}, "", err
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

func applyDailyActivity(ctx context.Context, st *store.Store, item map[string]any, fetchedAt string) error {
	rawID, err := st.InsertRawDocument(ctx, store.RawDocument{
		Provider:     "oura",
		DocumentKind: "daily_activity",
		ExternalID:   stringValue(item["id"]),
		LocalDate:    stringValue(item["day"]),
		Payload:      mustJSON(item),
		FetchedAt:    fetchedAt,
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
	rawID, err := st.InsertRawDocument(ctx, store.RawDocument{
		Provider:     "oura",
		DocumentKind: "daily_readiness",
		ExternalID:   stringValue(item["id"]),
		LocalDate:    stringValue(item["day"]),
		Payload:      mustJSON(item),
		FetchedAt:    fetchedAt,
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
	rawID, err := st.InsertRawDocument(ctx, store.RawDocument{
		Provider:     "oura",
		DocumentKind: "sleep",
		ExternalID:   stringValue(item["id"]),
		LocalDate:    stringValue(item["day"]),
		Payload:      mustJSON(item),
		FetchedAt:    fetchedAt,
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
