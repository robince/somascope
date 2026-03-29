package oura

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
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
	defaultRequestWindow      = 5 * time.Minute
	defaultRequestBudget      = 3000
	defaultSparseProbeDays    = 30
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
	kind          string
	featurePath   string
	queryMode     queryMode
	useCursor     bool
	requestWindow func(time.Time) syncRequestWindow
	normalize     func(context.Context, *store.Store, map[string]any, string, *int64) error
	sparseProbe   bool
}

type syncRequestWindow struct {
	params       url.Values
	requestStart string
	requestEnd   string
	logicalDate  string
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
	syncClient := client
	if client != nil && client.RateLimiter == nil && (client.HTTPClient == nil || client.HTTPClient.Transport == nil) {
		syncClient = client.WithRateLimiter(NewRequestPacer(defaultRequestBudget, defaultRequestWindow))
	}

	// Token refresh callback for mid-sync 401 handling.
	// Protected by mutex so concurrent goroutines don't trigger multiple refreshes.
	var tokenMu sync.Mutex
	onUnauthorized := func(ctx context.Context, staleToken string) (string, error) {
		tokenMu.Lock()
		defer tokenMu.Unlock()
		if activeConnection.AccessToken != staleToken {
			return activeConnection.AccessToken, nil
		}
		if activeConnection.RefreshToken == "" {
			return "", fmt.Errorf("no refresh token available")
		}
		refreshed, err := client.RefreshToken(ctx, cfg, activeConnection.RefreshToken)
		if err != nil {
			return "", err
		}
		activeConnection.AccessToken = refreshed.AccessToken
		if refreshed.RefreshToken != "" {
			activeConnection.RefreshToken = refreshed.RefreshToken
		}
		activeConnection.Scope = firstNonEmpty(refreshed.Scope, activeConnection.Scope)
		activeConnection.TokenExpiresAt = isoTime(refreshed.ExpiresAt)
		activeConnection.Status = "connected"
		_ = st.UpsertConnection(ctx, activeConnection)
		return activeConnection.AccessToken, nil
	}

	entities := syncEntities()

	overallStart := endDate
	entityStarts := make(map[string]time.Time)
	for _, entity := range entities {
		if entity.queryMode == queryModeNone {
			continue
		}
		entityStart, err := resolveEntityStart(ctx, st, entity.kind, options.StartDate, endDate, entity.useCursor)
		if err != nil {
			return err
		}
		entityStarts[entity.kind] = entityStart
		if entityStart.Before(overallStart) {
			overallStart = entityStart
		}
	}
	if err := tracker.SetEffectiveRange(overallStart.Format(dateLayout), endDate.Format(dateLayout)); err != nil {
		return err
	}

	for _, entity := range entities {
		if entity.queryMode != queryModeNone {
			continue
		}
		if err := syncEntityNoRange(ctx, st, syncClient, activeConnection.AccessToken, fetchedAt, entity, tracker, onUnauthorized); err != nil {
			return err
		}
	}

	type entityJob struct {
		entity    syncEntity
		startDate time.Time
	}
	jobs := make([]entityJob, 0, len(entities))
	for _, entity := range entities {
		if entity.queryMode == queryModeNone {
			continue
		}
		jobs = append(jobs, entityJob{
			entity:    entity,
			startDate: entityStarts[entity.kind],
		})
	}

	syncCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		errOnce  sync.Once
		firstErr error
	)
	for _, job := range jobs {
		job := job
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := syncEntityRange(syncCtx, st, syncClient, activeConnection.AccessToken, fetchedAt, job.entity, job.startDate, endDate, tracker, onUnauthorized); err != nil {
				errOnce.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func syncEntities() []syncEntity {
	return []syncEntity{
		{kind: "personal_info", featurePath: "/v2/usercollection/personal_info", queryMode: queryModeNone},
		{kind: "tag", featurePath: "/v2/usercollection/tag", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow, sparseProbe: true},
		{kind: "enhanced_tag", featurePath: "/v2/usercollection/enhanced_tag", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow, sparseProbe: true},
		{kind: "workout", featurePath: "/v2/usercollection/workout", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow, sparseProbe: true},
		{kind: "session", featurePath: "/v2/usercollection/session", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow, sparseProbe: true},
		{kind: "daily_activity", featurePath: "/v2/usercollection/daily_activity", queryMode: queryModeDate, useCursor: true, requestWindow: nextDayExclusiveDateWindow, normalize: applyDailyActivity},
		{kind: "daily_sleep", featurePath: "/v2/usercollection/daily_sleep", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow},
		{kind: "daily_spo2", featurePath: "/v2/usercollection/daily_spo2", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow},
		{kind: "daily_readiness", featurePath: "/v2/usercollection/daily_readiness", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow, normalize: applyDailyReadiness},
		{kind: "sleep", featurePath: "/v2/usercollection/sleep", queryMode: queryModeDate, useCursor: true, requestWindow: sleepResultDayWindow, normalize: applySleep},
		{kind: "sleep_time", featurePath: "/v2/usercollection/sleep_time", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow},
		{kind: "rest_mode_period", featurePath: "/v2/usercollection/rest_mode_period", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow, sparseProbe: true},
		{kind: "daily_stress", featurePath: "/v2/usercollection/daily_stress", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow},
		{kind: "daily_resilience", featurePath: "/v2/usercollection/daily_resilience", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow},
		{kind: "daily_cardiovascular_age", featurePath: "/v2/usercollection/daily_cardiovascular_age", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow},
		{kind: "vo2_max", featurePath: "/v2/usercollection/vO2_max", queryMode: queryModeDate, useCursor: true, requestWindow: inclusiveDateWindow, sparseProbe: true},
		{kind: "heartrate", featurePath: "/v2/usercollection/heartrate", queryMode: queryModeDateTime, useCursor: true, requestWindow: halfOpenDateTimeWindow},
	}
}

func syncEntityNoRange(ctx context.Context, st *store.Store, client *Client, accessToken, fetchedAt string, entity syncEntity, tracker *providersync.Tracker, onUnauthorized func(context.Context, string) (string, error)) error {
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
		var result DocumentResult
		result, err = client.FetchDocumentResult(ctx, accessToken, entity.featurePath, RetryConfig{
			MaxAttempts:    defaultRetryAttempts,
			OnRetry:        retryCallback(tracker, entity, "", ""),
			OnUnauthorized: onUnauthorized,
		})
		if err == nil {
			if _, archiveErr := archiveRawResponse(ctx, st, entity, syncRequestWindow{}, result.RawBody, fetchedAt, nil); archiveErr != nil {
				return archiveErr
			}
			if len(result.Value) > 0 {
				items = []map[string]any{result.Value}
			}
		}
	default:
		pages, pageErr := client.FetchCollectionPages(ctx, accessToken, entity.featurePath, nil, RetryConfig{
			MaxAttempts:    defaultRetryAttempts,
			OnRetry:        retryCallback(tracker, entity, "", ""),
			OnUnauthorized: onUnauthorized,
		})
		err = pageErr
		if err == nil {
			for _, page := range pages {
				if !shouldArchiveCollectionPage(page) {
					continue
				}
				if _, archiveErr := archiveRawResponse(ctx, st, entity, syncRequestWindow{
					params:       page.Query,
					requestStart: "",
					requestEnd:   "",
				}, page.RawBody, fetchedAt, nil); archiveErr != nil {
					return archiveErr
				}
				items = append(items, page.Data...)
			}
		}
	}
	if err != nil {
		return failEntity(tracker, entity, "", "", "fetch_collection", err)
	}

	for _, item := range items {
		if entity.normalize == nil {
			continue
		}
		if err := entity.normalize(ctx, st, item, fetchedAt, nil); err != nil {
			return failEntity(tracker, entity, "", "", "apply_document", err)
		}
	}
	if err := tracker.CompleteChunk(entity.kind, "", len(items)); err != nil {
		return err
	}
	return tracker.CompleteEntity(entity.kind)
}

func syncEntityRange(ctx context.Context, st *store.Store, client *Client, accessToken, fetchedAt string, entity syncEntity, startDate, endDate time.Time, tracker *providersync.Tracker, onUnauthorized func(context.Context, string) (string, error)) error {
	if startDate.After(endDate) {
		startDate = endDate
	}
	totalChunks := countChunks(startDate, endDate, 1)
	if err := tracker.StartEntity(entity.kind, startDate.Format(dateLayout), endDate.Format(dateLayout), totalChunks); err != nil {
		return err
	}

	if entity.sparseProbe {
		return syncSparseEntityRange(ctx, st, client, accessToken, fetchedAt, entity, startDate, endDate, tracker, onUnauthorized)
	}

	for day := startDate; !day.After(endDate); day = day.AddDate(0, 0, 1) {
		if err := syncEntityDay(ctx, st, client, accessToken, fetchedAt, entity, day, tracker, onUnauthorized); err != nil {
			return err
		}
	}

	return tracker.CompleteEntity(entity.kind)
}

func syncSparseEntityRange(ctx context.Context, st *store.Store, client *Client, accessToken, fetchedAt string, entity syncEntity, startDate, endDate time.Time, tracker *providersync.Tracker, onUnauthorized func(context.Context, string) (string, error)) error {
	for blockStart := startDate; !blockStart.After(endDate); blockStart = blockStart.AddDate(0, 0, defaultSparseProbeDays) {
		blockEnd := minDate(endDate, blockStart.AddDate(0, 0, defaultSparseProbeDays-1))
		if err := syncSparseProbeBlock(ctx, st, client, accessToken, fetchedAt, entity, blockStart, blockEnd, tracker, onUnauthorized); err != nil {
			return err
		}
	}
	return tracker.CompleteEntity(entity.kind)
}

func syncSparseProbeBlock(ctx context.Context, st *store.Store, client *Client, accessToken, fetchedAt string, entity syncEntity, startDate, endDate time.Time, tracker *providersync.Tracker, onUnauthorized func(context.Context, string) (string, error)) error {
	if startDate.After(endDate) {
		return nil
	}
	if startDate.Equal(endDate) {
		return syncEntityDay(ctx, st, client, accessToken, fetchedAt, entity, startDate, tracker, onUnauthorized)
	}

	chunkStart := startDate.Format(dateLayout)
	chunkEnd := endDate.Format(dateLayout)
	window := inclusiveDateSpanWindow(startDate, endDate)
	pages, err := client.FetchCollectionPages(ctx, accessToken, entity.featurePath, window.params, RetryConfig{
		MaxAttempts:    defaultRetryAttempts,
		OnRetry:        retryCallback(tracker, entity, chunkStart, chunkEnd),
		OnUnauthorized: onUnauthorized,
	})
	if err != nil {
		return failEntity(tracker, entity, chunkStart, chunkEnd, "fetch_collection", err)
	}
	if !collectionPagesContainData(pages) {
		return skipSparseRange(ctx, st, fetchedAt, entity, startDate, endDate, tracker)
	}

	dayCount := daysInclusive(startDate, endDate)
	leftCount := dayCount / 2
	leftEnd := startDate.AddDate(0, 0, leftCount-1)
	rightStart := leftEnd.AddDate(0, 0, 1)
	if err := syncSparseProbeBlock(ctx, st, client, accessToken, fetchedAt, entity, startDate, leftEnd, tracker, onUnauthorized); err != nil {
		return err
	}
	return syncSparseProbeBlock(ctx, st, client, accessToken, fetchedAt, entity, rightStart, endDate, tracker, onUnauthorized)
}

func skipSparseRange(ctx context.Context, st *store.Store, fetchedAt string, entity syncEntity, startDate, endDate time.Time, tracker *providersync.Tracker) error {
	for day := startDate; !day.After(endDate); day = day.AddDate(0, 0, 1) {
		chunkLabel := day.Format(dateLayout)
		if err := tracker.StartChunk(entity.kind, chunkLabel, chunkLabel); err != nil {
			return err
		}
		cursor := ""
		if entity.useCursor {
			cursor = chunkLabel
			if err := st.UpsertSyncState(ctx, "oura", entity.kind, cursor, fetchedAt); err != nil {
				return failEntity(tracker, entity, chunkLabel, chunkLabel, "update_sync_state", err)
			}
		}
		if err := tracker.CompleteChunk(entity.kind, cursor, 0); err != nil {
			return err
		}
	}
	return nil
}

func syncEntityDay(ctx context.Context, st *store.Store, client *Client, accessToken, fetchedAt string, entity syncEntity, day time.Time, tracker *providersync.Tracker, onUnauthorized func(context.Context, string) (string, error)) error {
	window := entity.requestWindow(day)
	chunkLabel := window.logicalDate
	if chunkLabel == "" {
		chunkLabel = day.Format(dateLayout)
	}
	if err := tracker.StartChunk(entity.kind, chunkLabel, chunkLabel); err != nil {
		return err
	}

	pages, err := client.FetchCollectionPages(ctx, accessToken, entity.featurePath, window.params, RetryConfig{
		MaxAttempts:    defaultRetryAttempts,
		OnRetry:        retryCallback(tracker, entity, chunkLabel, chunkLabel),
		OnUnauthorized: onUnauthorized,
	})
	if err != nil {
		return failEntity(tracker, entity, chunkLabel, chunkLabel, "fetch_collection", err)
	}

	rowsWritten := 0
	for _, page := range pages {
		if !shouldArchiveCollectionPage(page) {
			continue
		}
		pageWindow := window
		pageWindow.params = page.Query
		rawID, archiveErr := archiveRawResponse(ctx, st, entity, pageWindow, page.RawBody, fetchedAt, &day)
		if archiveErr != nil {
			return failEntity(tracker, entity, chunkLabel, chunkLabel, "archive_raw_response", archiveErr)
		}

		if entity.normalize == nil {
			rowsWritten++
			continue
		}

		for _, item := range page.Data {
			if err := entity.normalize(ctx, st, item, fetchedAt, &rawID); err != nil {
				return failEntity(tracker, entity, chunkLabel, chunkLabel, "apply_document", err)
			}
			rowsWritten++
		}
	}

	cursor := ""
	if entity.useCursor {
		cursor = chunkLabel
		if err := st.UpsertSyncState(ctx, "oura", entity.kind, cursor, fetchedAt); err != nil {
			return failEntity(tracker, entity, chunkLabel, chunkLabel, "update_sync_state", err)
		}
	}
	if err := tracker.CompleteChunk(entity.kind, cursor, rowsWritten); err != nil {
		return err
	}
	return nil
}

func shouldArchiveCollectionPage(page CollectionPage) bool {
	return len(page.Data) > 0 || strings.TrimSpace(page.NextToken) != ""
}

func collectionPagesContainData(pages []CollectionPage) bool {
	return slices.ContainsFunc(pages, shouldArchiveCollectionPage)
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

func inclusiveDateWindow(day time.Time) syncRequestWindow {
	date := day.Format(dateLayout)
	params := url.Values{}
	params.Set("start_date", date)
	params.Set("end_date", date)
	return syncRequestWindow{
		params:       params,
		requestStart: date,
		requestEnd:   date,
		logicalDate:  date,
	}
}

func inclusiveDateSpanWindow(startDate, endDate time.Time) syncRequestWindow {
	start := startDate.Format(dateLayout)
	end := endDate.Format(dateLayout)
	params := url.Values{}
	params.Set("start_date", start)
	params.Set("end_date", end)
	return syncRequestWindow{
		params:       params,
		requestStart: start,
		requestEnd:   end,
	}
}

func nextDayExclusiveDateWindow(day time.Time) syncRequestWindow {
	start := day.Format(dateLayout)
	end := day.AddDate(0, 0, 1).Format(dateLayout)
	params := url.Values{}
	params.Set("start_date", start)
	params.Set("end_date", end)
	return syncRequestWindow{
		params:       params,
		requestStart: start,
		requestEnd:   end,
		logicalDate:  start,
	}
}

func sleepResultDayWindow(day time.Time) syncRequestWindow {
	start := day.AddDate(0, 0, -1).Format(dateLayout)
	end := day.Format(dateLayout)
	params := url.Values{}
	params.Set("start_date", start)
	params.Set("end_date", end)
	return syncRequestWindow{
		params:       params,
		requestStart: start,
		requestEnd:   end,
		logicalDate:  end,
	}
}

func halfOpenDateTimeWindow(day time.Time) syncRequestWindow {
	start := day.UTC()
	end := day.AddDate(0, 0, 1).UTC()
	params := url.Values{}
	params.Set("start_datetime", start.Format(time.RFC3339))
	params.Set("end_datetime", end.Format(time.RFC3339))
	return syncRequestWindow{
		params:       params,
		requestStart: start.Format(time.RFC3339),
		requestEnd:   end.Format(time.RFC3339),
		logicalDate:  day.Format(dateLayout),
	}
}

func archiveRawResponse(ctx context.Context, st *store.Store, entity syncEntity, window syncRequestWindow, payload json.RawMessage, fetchedAt string, fallbackDay *time.Time) (int64, error) {
	localDate := strings.TrimSpace(window.logicalDate)
	if localDate == "" && fallbackDay != nil {
		localDate = fallbackDay.Format(dateLayout)
	}

	return st.UpsertRawDocument(ctx, store.RawDocument{
		Provider:     "oura",
		DocumentKind: entity.kind,
		LocalDate:    localDate,
		RequestPath:  entity.featurePath,
		RequestQuery: encodeValues(window.params),
		RequestStart: window.requestStart,
		RequestEnd:   window.requestEnd,
		Payload:      payload,
		FetchedAt:    fetchedAt,
		DocumentKey:  requestDocumentKey(entity.kind, entity.featurePath, window.params),
	})
}

func applyDailyActivity(ctx context.Context, st *store.Store, item map[string]any, _ string, rawDocumentID *int64) error {
	return st.UpsertDailyRecord(ctx, store.DailyRecord{
		Provider:      "oura",
		RecordKind:    "daily_activity",
		LocalDate:     stringValue(item["day"]),
		ZoneOffset:    zoneOffset(item["timestamp"]),
		SourceDevice:  "oura-ring",
		ExternalID:    stringValue(item["id"]),
		Summary:       normalizeDailyActivity(item),
		RawDocumentID: rawDocumentID,
	})
}

func applyDailyReadiness(ctx context.Context, st *store.Store, item map[string]any, _ string, rawDocumentID *int64) error {
	return st.UpsertDailyRecord(ctx, store.DailyRecord{
		Provider:      "oura",
		RecordKind:    "daily_readiness",
		LocalDate:     stringValue(item["day"]),
		ZoneOffset:    zoneOffset(item["timestamp"]),
		SourceDevice:  "oura-ring",
		ExternalID:    stringValue(item["id"]),
		Summary:       normalizeDailyReadiness(item),
		RawDocumentID: rawDocumentID,
	})
}

func applySleep(ctx context.Context, st *store.Store, item map[string]any, _ string, rawDocumentID *int64) error {
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
		IsNap:             strings.EqualFold(stringValue(item["type"]), "short_sleep"),
		Stages:            normalizeSleepStages(item),
		Metrics:           normalizeSleepMetrics(item),
		RawDocumentID:     rawDocumentID,
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
	minutes := int(math.Round(number / 60))
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

func encodeValues(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	return values.Encode()
}

func requestDocumentKey(documentKind, path string, params url.Values) string {
	raw := documentKind + "|" + path + "|" + encodeValues(params)
	sum := sha1.Sum([]byte(raw))
	return "request:sha1:" + hex.EncodeToString(sum[:])
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

func daysInclusive(startDate, endDate time.Time) int {
	if startDate.After(endDate) {
		return 0
	}
	return int(endDate.Sub(startDate).Hours()/24) + 1
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
	return errors.As(err, target)
}
