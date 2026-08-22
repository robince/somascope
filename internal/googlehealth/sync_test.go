package googlehealth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robince/somascope/internal/providersync"
	"github.com/robince/somascope/internal/store"
)

func cleanupStore(t *testing.T, closer interface{ Close() error }) {
	t.Helper()
	t.Cleanup(func() {
		if err := closer.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
}

func TestListDataPointsRequestsMaxPageSize(t *testing.T) {
	var gotPageSize string
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotPageSize = req.URL.Query().Get("pageSize")
		return jsonResponse(`{"dataPoints":[]}`), nil
	})})

	if _, err := client.ListDataPoints(context.Background(), "token", "heart-rate", url.Values{}, RetryConfig{MaxAttempts: 1}); err != nil {
		t.Fatalf("list data points: %v", err)
	}
	if gotPageSize != "10000" {
		t.Fatalf("expected pageSize=10000, got %q", gotPageSize)
	}
}

func TestReconcileDataPointsUsesSleepPageLimit(t *testing.T) {
	var gotPageSize string
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotPageSize = req.URL.Query().Get("pageSize")
		return jsonResponse(`{"dataPoints":[]}`), nil
	})})

	if _, err := client.ReconcileDataPoints(context.Background(), "token", "sleep", url.Values{}, RetryConfig{MaxAttempts: 1}); err != nil {
		t.Fatalf("reconcile sleep data points: %v", err)
	}
	if gotPageSize != "25" {
		t.Fatalf("expected pageSize=25, got %q", gotPageSize)
	}
}

func TestAuthorizationURLIncludesPKCEAndOfflineAccess(t *testing.T) {
	client := NewClient(nil)
	url, err := client.AuthorizationURL(AppConfig{
		ClientID:    "client-1",
		RedirectURI: "http://localhost:18080/oauth/google_health/callback",
	}, "state-1", "challenge-1")
	if err != nil {
		t.Fatalf("authorization url: %v", err)
	}
	for _, want := range []string{
		"accounts.google.com",
		"access_type=offline",
		"prompt=consent",
		"code_challenge=challenge-1",
		"code_challenge_method=S256",
		"googlehealth.activity_and_fitness.readonly",
	} {
		if !strings.Contains(url, want) {
			t.Fatalf("expected authorize url to contain %q, got %s", want, url)
		}
	}
}

func TestMergeActivityPointReadsLiveGoogleHealthRollupFields(t *testing.T) {
	summary := map[string]any{"day": "2026-08-18"}
	mergeActivityPoint(summary, "distance", map[string]any{
		"distance": map[string]any{"millimetersSum": "7597389"},
	})
	mergeActivityPoint(summary, "active-minutes", map[string]any{
		"activeMinutes": map[string]any{
			"activeMinutesRollupByActivityLevel": []any{
				map[string]any{"activityLevel": "LIGHT", "activeMinutesSum": "196"},
				map[string]any{"activityLevel": "MODERATE", "activeMinutesSum": "15"},
				map[string]any{"activityLevel": "VIGOROUS", "activeMinutesSum": "30"},
			},
		},
	})
	mergeActivityPoint(summary, "sedentary-period", map[string]any{
		"sedentaryPeriod": map[string]any{"durationSum": "28680s"},
	})

	if got := intFromAny(summary["equivalent_walking_distance"]); got == nil || *got != 7597 {
		t.Fatalf("distance: %+v", summary)
	}
	if got := intFromAny(summary["low_activity_minutes"]); got == nil || *got != 196 {
		t.Fatalf("light minutes: %+v", summary)
	}
	if got := intFromAny(summary["medium_activity_minutes"]); got == nil || *got != 15 {
		t.Fatalf("moderate minutes: %+v", summary)
	}
	if got := intFromAny(summary["high_activity_minutes"]); got == nil || *got != 30 {
		t.Fatalf("vigorous minutes: %+v", summary)
	}
	if got := intFromAny(summary["resting_minutes"]); got == nil || *got != 478 {
		t.Fatalf("sedentary minutes: %+v", summary)
	}
}

func TestMergeActiveMinutesDoesNotTreatTotalAsModerate(t *testing.T) {
	summary := map[string]any{}
	mergeActiveMinutes(summary, map[string]any{"minutesSum": "47"})

	if _, ok := summary["medium_activity_minutes"]; ok {
		t.Fatalf("expected no moderate bucket without an activity-level breakdown")
	}
	if got := summary["total_active_minutes"]; got != 47 {
		t.Fatalf("expected total active minutes to be preserved, got %#v", got)
	}
}

func TestFilterECGPagesHonorsRequestedEndDate(t *testing.T) {
	point := func(timestamp string) map[string]any {
		return map[string]any{"electrocardiogram": map[string]any{"interval": map[string]any{"startTime": timestamp}}}
	}
	pages := []ListPage{{
		DataPoints: []map[string]any{
			point("2026-08-20T12:00:00Z"),
			point("2026-08-21T12:00:00Z"),
			point("2026-08-22T12:00:00Z"),
		},
		RawBody: json.RawMessage(`{"dataPoints":[{"electrocardiogram":{"interval":{"startTime":"2026-08-20T12:00:00Z"}}},{"electrocardiogram":{"interval":{"startTime":"2026-08-21T12:00:00Z"}}},{"electrocardiogram":{"interval":{"startTime":"2026-08-22T12:00:00Z"}}}],"nextPageToken":"","totalSize":9007199254740993}`),
	}}

	start, _ := time.Parse(dateLayout, "2026-08-20")
	end, _ := time.Parse(dateLayout, "2026-08-21")
	filtered, err := filterECGPages(pages, start, end)
	if err != nil {
		t.Fatalf("filter ECG pages: %v", err)
	}
	if got := len(filtered[0].DataPoints); got != 2 {
		t.Fatalf("expected 2 in-range ECG points, got %d", got)
	}
	var raw struct {
		DataPoints []map[string]any `json:"dataPoints"`
	}
	if err := json.Unmarshal(filtered[0].RawBody, &raw); err != nil {
		t.Fatalf("decode filtered raw payload: %v", err)
	}
	if got := len(raw.DataPoints); got != 2 {
		t.Fatalf("expected raw payload to contain 2 in-range points, got %d", got)
	}
	if !strings.Contains(string(filtered[0].RawBody), `"totalSize":9007199254740993`) {
		t.Fatalf("expected large numeric text to be preserved, got %s", filtered[0].RawBody)
	}
}

func TestResolveEntityStartDateClampsCursorOverlapToEnd(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	if err := st.UpsertSyncState(context.Background(), Provider, "daily_activity", "2026-08-22", "2026-08-22T12:00:00Z"); err != nil {
		t.Fatalf("seed sync state: %v", err)
	}
	end, _ := time.Parse(dateLayout, "2026-08-10")

	start, err := resolveEntityStartDate(context.Background(), st, "daily_activity", "", end)
	if err != nil {
		t.Fatalf("resolve start date: %v", err)
	}
	if !start.Equal(end) {
		t.Fatalf("expected start to clamp to %s, got %s", end.Format(dateLayout), start.Format(dateLayout))
	}
}

func TestResolveEntityStartDateUsesIndependentCursors(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	ctx := context.Background()
	if err := st.UpsertSyncState(ctx, Provider, "daily_activity", "2026-08-20", "2026-08-20T12:00:00Z"); err != nil {
		t.Fatalf("seed activity state: %v", err)
	}
	if err := st.UpsertSyncState(ctx, Provider, "sleep", "2026-07-15", "2026-07-15T12:00:00Z"); err != nil {
		t.Fatalf("seed sleep state: %v", err)
	}
	end, _ := time.Parse(dateLayout, "2026-08-22")
	activityStart, err := resolveEntityStartDate(ctx, st, "daily_activity", "", end)
	if err != nil {
		t.Fatalf("resolve activity start: %v", err)
	}
	sleepStart, err := resolveEntityStartDate(ctx, st, "sleep", "", end)
	if err != nil {
		t.Fatalf("resolve sleep start: %v", err)
	}
	rawStart, err := resolveEntityStartDate(ctx, st, "heartrate", "", end)
	if err != nil {
		t.Fatalf("resolve raw start: %v", err)
	}
	if got := activityStart.Format(dateLayout); got != "2026-08-17" {
		t.Fatalf("expected activity overlap start 2026-08-17, got %s", got)
	}
	if got := sleepStart.Format(dateLayout); got != "2026-07-12" {
		t.Fatalf("expected independent sleep overlap start 2026-07-12, got %s", got)
	}
	if got := rawStart.Format(dateLayout); got != "2026-07-24" {
		t.Fatalf("expected missing raw cursor to bootstrap at 2026-07-24, got %s", got)
	}
}

func TestAdvanceSyncStateDoesNotRewindCursor(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	if err := st.UpsertSyncState(context.Background(), Provider, "daily_activity", "2026-08-22", "2026-08-22T12:00:00Z"); err != nil {
		t.Fatalf("seed sync state: %v", err)
	}

	if err := advanceSyncState(context.Background(), st, "daily_activity", "2026-08-10", "2026-08-23T12:00:00Z"); err != nil {
		t.Fatalf("advance sync state: %v", err)
	}
	cursor, _, err := st.SyncState(context.Background(), Provider, "daily_activity")
	if err != nil {
		t.Fatalf("load sync state: %v", err)
	}
	if cursor != "2026-08-22" {
		t.Fatalf("expected cursor to remain at 2026-08-22, got %q", cursor)
	}
}

func TestSleepLocalDateUsesCivilOffsetNotUTC(t *testing.T) {
	session, ok := sleepSessionFrom(map[string]any{
		"sleep": map[string]any{
			"interval": map[string]any{
				"startTime":    "2026-08-17T22:30:00Z",
				"endTime":      "2026-08-17T23:30:00Z",
				"endUtcOffset": "3600s",
			},
			"metadata": map[string]any{"nap": false},
			"summary":  map[string]any{"minutesAsleep": "60", "minutesInSleepPeriod": "60"},
		},
	})
	if !ok {
		t.Fatal("expected sleep session")
	}
	if session.LocalDate != "2026-08-18" {
		t.Fatalf("expected local wake date 2026-08-18 from UTC+1 offset, got %s", session.LocalDate)
	}
}

func TestSleepSessionFromUsesNapFlag(t *testing.T) {
	session, ok := sleepSessionFrom(map[string]any{
		"dataPointName": "users/me/dataTypes/sleep/dataPoints/1154146694073292232",
		"sleep": map[string]any{
			"interval": map[string]any{
				"startTime":      "2026-08-18T21:04:00Z",
				"startUtcOffset": "3600s",
				"endTime":        "2026-08-19T04:46:00Z",
				"endUtcOffset":   "3600s",
			},
			"type":     "STAGES",
			"metadata": map[string]any{"nap": false},
			"summary": map[string]any{
				"minutesAsleep":        "449",
				"minutesInSleepPeriod": "462",
			},
		},
	})
	if !ok {
		t.Fatal("expected sleep session")
	}
	if session.IsNap {
		t.Fatal("expected nap false to be stored as overnight sleep")
	}
	if session.ExternalID != "1154146694073292232" {
		t.Fatalf("unexpected external id: %s", session.ExternalID)
	}
	if session.ZoneOffset != "+01:00" {
		t.Fatalf("expected +01:00 zone offset, got %s", session.ZoneOffset)
	}
	if session.StartTime != "2026-08-18T22:04:00.000+01:00" {
		t.Fatalf("expected local start time, got %s", session.StartTime)
	}
	if session.EndTime != "2026-08-19T05:46:00.000+01:00" {
		t.Fatalf("expected local end time, got %s", session.EndTime)
	}
}

func TestSleepSessionFromClassifiesNap(t *testing.T) {
	session, ok := sleepSessionFrom(map[string]any{
		"sleep": map[string]any{
			"interval": map[string]any{
				"startTime": "2026-08-19T13:00:00Z",
				"endTime":   "2026-08-19T13:45:00Z",
			},
			"metadata": map[string]any{"nap": true},
			"summary":  map[string]any{"minutesAsleep": "40", "minutesInSleepPeriod": "45"},
		},
	})
	if !ok || !session.IsNap {
		t.Fatalf("expected nap metadata to produce a nap session: %#v", session)
	}
}

func TestGoogleHealthFiltersUseSupportedComparators(t *testing.T) {
	start, _ := time.Parse(dateLayout, "2026-08-17")
	end, _ := time.Parse(dateLayout, "2026-08-19")

	if got, want := civilDateRangeFilter("sleep.interval.civil_end_time", start, end), `sleep.interval.civil_end_time >= "2026-08-17" AND sleep.interval.civil_end_time < "2026-08-20"`; got != want {
		t.Fatalf("sleep filter:\n got %s\nwant %s", got, want)
	}
	if got, want := rawFilter(rawEntity{dataType: "daily-resting-heart-rate", query: rawQueryDaily}, start, end), `daily_resting_heart_rate.date >= "2026-08-17" AND daily_resting_heart_rate.date < "2026-08-20"`; got != want {
		t.Fatalf("daily vital filter:\n got %s\nwant %s", got, want)
	}
	if got := kebabToSnake("daily-resting-heart-rate"); got != "daily_resting_heart_rate" {
		t.Fatalf("unexpected snake case: %q", got)
	}
	if got, want := rawFilter(rawEntity{dataType: "heart-rate", query: rawQuerySample}, start, start), `heart_rate.sample_time.physical_time >= "2026-08-17T00:00:00Z" AND heart_rate.sample_time.physical_time < "2026-08-18T00:00:00Z"`; got != want {
		t.Fatalf("heart rate filter:\n got %s\nwant %s", got, want)
	}
	if got, want := rawFilter(rawEntity{dataType: "exercise", query: rawQueryInterval}, start, end), `exercise.interval.civil_start_time >= "2026-08-17" AND exercise.interval.civil_start_time < "2026-08-20"`; got != want {
		t.Fatalf("exercise filter:\n got %s\nwant %s", got, want)
	}
}

func TestSyncNormalizesDailyActivityAndSleepAndArchivesHeartrate(t *testing.T) {
	app, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	cleanupStore(t, app)

	manager, err := providersync.NewManager(app)
	if err != nil {
		t.Fatalf("sync manager: %v", err)
	}
	t.Cleanup(manager.Shutdown)

	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.Contains(req.URL.Path, "/dataTypes/steps/dataPoints:dailyRollUp"):
			return jsonResponse(`{"rollupDataPoints":[{"civilStartTime":{"date":{"year":2026,"month":3,"day":20}},"steps":{"countSum":"8123"},"dataSource":{"device":{"displayName":"Charge 6"}}}]}`), nil
		case strings.Contains(req.URL.Path, "/dataTypes/active-energy-burned/dataPoints:dailyRollUp"):
			return jsonResponse(`{"rollupDataPoints":[{"civilStartTime":{"date":{"year":2026,"month":3,"day":20}},"activeEnergyBurned":{"kcalSum":410}}]}`), nil
		case strings.Contains(req.URL.Path, "/dataTypes/total-calories/dataPoints:dailyRollUp"):
			return jsonResponse(`{"rollupDataPoints":[{"civilStartTime":{"date":{"year":2026,"month":3,"day":20}},"totalCalories":{"kcalSum":2400}}]}`), nil
		case strings.Contains(req.URL.Path, "/dataTypes/distance/dataPoints:dailyRollUp"):
			return jsonResponse(`{"rollupDataPoints":[{"civilStartTime":{"date":{"year":2026,"month":3,"day":20}},"distance":{"millimetersSum":"6100000"}}]}`), nil
		case strings.Contains(req.URL.Path, "/dataTypes/active-minutes/dataPoints:dailyRollUp"):
			return jsonResponse(`{"rollupDataPoints":[{"civilStartTime":{"date":{"year":2026,"month":3,"day":20}},"activeMinutes":{"activeMinutesRollupByActivityLevel":[{"activityLevel":"LIGHT","activeMinutesSum":"20"},{"activityLevel":"MODERATE","activeMinutesSum":"48"},{"activityLevel":"VIGOROUS","activeMinutesSum":"10"}]}}]}`), nil
		case strings.Contains(req.URL.Path, "/dataTypes/sedentary-period/dataPoints:dailyRollUp"):
			return jsonResponse(`{"rollupDataPoints":[{"civilStartTime":{"date":{"year":2026,"month":3,"day":20}},"sedentaryPeriod":{"durationSum":"30000s"}}]}`), nil
		case strings.Contains(req.URL.Path, "/dataTypes/sleep/dataPoints:reconcile"):
			filter := req.URL.Query().Get("filter")
			if strings.Contains(filter, "<=") {
				return jsonResponseStatus(http.StatusBadRequest, `{"error":{"code":400,"message":"Invalid data point filter: INVALID_DATA_POINT_FILTER_RESTRICTION_COMPARATOR."}}`), nil
			}
			return jsonResponse(`{"dataPoints":[{"dataPointName":"users/me/dataTypes/sleep/dataPoints/sleep-1","dataSource":{"device":{"displayName":"Charge 6"}},"sleep":{"interval":{"startTime":"2026-03-19T22:10:00Z","endTime":"2026-03-20T06:12:00Z","civilEndTime":{"date":{"year":2026,"month":3,"day":20}}},"type":"STAGES","metadata":{"nap":false},"summary":{"minutesAsleep":"430","minutesInSleepPeriod":"482","stagesSummary":[{"type":"DEEP","minutes":"90"},{"type":"LIGHT","minutes":"200"},{"type":"REM","minutes":"80"},{"type":"AWAKE","minutes":"52"}]}}}]}`), nil
		case strings.Contains(req.URL.Path, "/dataTypes/daily-resting-heart-rate/dataPoints"):
			filter := req.URL.Query().Get("filter")
			if strings.Contains(filter, "dailyRestingHeartRate") || !strings.Contains(filter, "daily_resting_heart_rate.date") {
				return jsonResponseStatus(http.StatusBadRequest, `{"error":{"code":400,"message":"Invalid data point filter: INVALID_DATA_POINT_FILTER_DATA_TYPE_RESTRICTION."}}`), nil
			}
			return jsonResponse(`{"dataPoints":[]}`), nil
		case strings.Contains(req.URL.Path, "/dataTypes/heart-rate/dataPoints"):
			filter := req.URL.Query().Get("filter")
			if strings.Contains(filter, "heartRate.") || !strings.Contains(filter, "heart_rate.sample_time.physical_time") {
				return jsonResponseStatus(http.StatusBadRequest, `{"error":{"code":400,"message":"Invalid data point filter: INVALID_DATA_POINT_FILTER_DATA_TYPE_RESTRICTION."}}`), nil
			}
			return jsonResponse(`{"dataPoints":[{"heartRate":{"bpm":61}}]}`), nil
		default:
			return jsonResponse(`{"dataPoints":[]}`), nil
		}
	})})

	connection := store.Connection{
		Provider:    Provider,
		AccessToken: "token",
		Status:      "connected",
	}
	if err := app.UpsertConnection(context.Background(), connection); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	run, already, err := manager.Start(Provider, "backfill", "2026-03-20", "2026-03-20", func(ctx context.Context, tracker *providersync.Tracker) error {
		return Sync(ctx, app, client, AppConfig{}, connection, SyncOptions{
			StartDate: "2026-03-20",
			EndDate:   "2026-03-20",
			Tracker:   tracker,
		})
	})
	if err != nil {
		t.Fatalf("start sync: %v", err)
	}
	if already {
		t.Fatalf("did not expect an already running sync")
	}

	waitForRun(t, app, run.ID)

	records, err := app.RecentDailyRecords(context.Background(), Provider, 10)
	if err != nil {
		t.Fatalf("recent daily records: %v", err)
	}
	if len(records) != 1 || records[0].RecordKind != "daily_activity" {
		t.Fatalf("unexpected daily records: %+v", records)
	}
	var summary map[string]any
	if err := json.Unmarshal(records[0].Summary, &summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if intFromAny(summary["steps"]) == nil || *intFromAny(summary["steps"]) != 8123 {
		t.Fatalf("unexpected steps: %+v", summary)
	}
	if intFromAny(summary["medium_activity_minutes"]) == nil || *intFromAny(summary["medium_activity_minutes"]) != 48 {
		t.Fatalf("expected moderate activity minutes stored as minutes, got %+v", summary)
	}
	if intFromAny(summary["low_activity_minutes"]) == nil || *intFromAny(summary["low_activity_minutes"]) != 20 {
		t.Fatalf("expected light activity minutes, got %+v", summary)
	}
	if intFromAny(summary["high_activity_minutes"]) == nil || *intFromAny(summary["high_activity_minutes"]) != 10 {
		t.Fatalf("expected vigorous activity minutes, got %+v", summary)
	}
	if intFromAny(summary["equivalent_walking_distance"]) == nil || *intFromAny(summary["equivalent_walking_distance"]) != 6100 {
		t.Fatalf("expected distance converted from millimeters, got %+v", summary)
	}
	if intFromAny(summary["resting_minutes"]) == nil || *intFromAny(summary["resting_minutes"]) != 500 {
		t.Fatalf("expected sedentary duration converted to minutes, got %+v", summary)
	}

	sessions, err := app.RecentSleepSessions(context.Background(), Provider, 10)
	if err != nil {
		t.Fatalf("recent sleep: %v", err)
	}
	if len(sessions) != 1 || sessions[0].IsNap {
		t.Fatalf("unexpected sleep sessions: %+v", sessions)
	}
	var stages map[string]any
	if err := json.Unmarshal(sessions[0].Stages, &stages); err != nil {
		t.Fatalf("decode stages: %v", err)
	}
	if intFromAny(stages["deep_minutes"]) == nil || *intFromAny(stages["deep_minutes"]) != 90 {
		t.Fatalf("expected sleep stages in minutes, got %+v", stages)
	}

	raw, err := app.RawExportRows(context.Background(), Provider, store.RawExportFilter{})
	if err != nil {
		t.Fatalf("raw export: %v", err)
	}
	foundHR := false
	for _, row := range raw {
		if row.DocumentKind == "heartrate" {
			foundHR = true
		}
	}
	if !foundHR {
		t.Fatalf("expected heartrate raw archive, got %+v", raw)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(body string) *http.Response {
	return jsonResponseStatus(http.StatusOK, body)
}

func jsonResponseStatus(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func waitForRun(t *testing.T, app *store.Store, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err := app.LatestFinishedSyncRunByProvider(context.Background(), Provider)
		if err == nil && run.ID == runID && run.Status != "running" {
			if run.Status != "succeeded" {
				t.Fatalf("sync finished with status %s error=%+v", run.Status, run.LastError)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for sync run %s", runID)
}
