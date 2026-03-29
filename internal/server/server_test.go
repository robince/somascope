package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/robince/somascope/internal/config"
	"github.com/robince/somascope/internal/oura"
	"github.com/robince/somascope/internal/store"
)

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true, got %v", payload["ok"])
	}
}

func TestAppEndpointIncludesAuthMode(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/app", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got := payload["auth_mode"]; got != string(config.AuthModeBYO) {
		t.Fatalf("expected auth_mode %q, got %#v", config.AuthModeBYO, got)
	}
}

func TestSettingsEndpointReturnsDefaults(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var payload struct {
		Providers []struct {
			Provider   string `json:"provider"`
			Configured bool   `json:"configured"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(payload.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(payload.Providers))
	}
	if payload.Providers[0].Configured || payload.Providers[1].Configured {
		t.Fatalf("expected default providers to be unconfigured, got %+v", payload.Providers)
	}
}

func TestSettingsPutPreservesStoredSecretWhenBlank(t *testing.T) {
	srv := newTestServer(t)

	saveJSON(t, srv, `{
		"user_timezone":"Europe/Paris",
		"providers":[
			{
				"provider":"fitbit",
				"client_id":"fitbit-client",
				"client_secret":"secret-one",
				"redirect_uri":"http://localhost:18080/oauth/fitbit/callback",
				"default_scopes":"activity heartrate sleep profile",
				"notes":"Fitbit notes"
			},
			{
				"provider":"oura",
				"client_id":"",
				"client_secret":"",
				"redirect_uri":"http://localhost:18080/oauth/oura/callback",
				"default_scopes":"email personal daily heartrate tag workout session spo2",
				"notes":"Oura notes"
			}
		]
	}`)

	resp := saveJSON(t, srv, `{
		"user_timezone":"Europe/Paris",
		"providers":[
			{
				"provider":"fitbit",
				"client_id":"fitbit-client",
				"client_secret":"",
				"redirect_uri":"http://localhost:18080/oauth/fitbit/callback",
				"default_scopes":"activity heartrate sleep profile",
				"notes":"Fitbit notes"
			},
			{
				"provider":"oura",
				"client_id":"",
				"client_secret":"",
				"redirect_uri":"http://localhost:18080/oauth/oura/callback",
				"default_scopes":"email personal daily heartrate tag workout session spo2",
				"notes":"Oura notes"
			}
		]
	}`)

	var payload struct {
		Providers []struct {
			Provider   string `json:"provider"`
			Configured bool   `json:"configured"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !payload.Providers[0].Configured {
		t.Fatalf("expected fitbit provider to remain configured")
	}
}

func TestCanonicalExportJSONL(t *testing.T) {
	srv := newTestServer(t)

	if err := srv.store.UpsertDailyRecord(context.Background(), store.DailyRecord{
		Provider:     "oura",
		RecordKind:   "daily_activity",
		LocalDate:    "2026-03-20",
		ZoneOffset:   "+01:00",
		SourceDevice: "oura-ring-4",
		ExternalID:   "activity-1",
		Summary:      json.RawMessage(`{"steps":12345}`),
	}); err != nil {
		t.Fatalf("seed daily record: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/export/canonical?format=jsonl", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"record_type":"daily_record"`) {
		t.Fatalf("expected jsonl body to include daily record row, got %s", rec.Body.String())
	}
}

func TestOuraAuthStartReturnsAuthorizeURL(t *testing.T) {
	srv := newTestServer(t)

	saveJSON(t, srv, `{
		"user_timezone":"Europe/London",
		"providers":[
			{
				"provider":"fitbit",
				"client_id":"",
				"client_secret":"",
				"redirect_uri":"http://localhost:18080/oauth/fitbit/callback",
				"default_scopes":"activity heartrate sleep profile",
				"notes":"Fitbit notes"
			},
			{
				"provider":"oura",
				"client_id":"oura-client",
				"client_secret":"oura-secret",
				"redirect_uri":"http://localhost:18080/oauth/oura/callback",
				"default_scopes":"email personal daily",
				"notes":"Oura notes"
			}
		]
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/oura/auth/start", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	parsed, err := url.Parse(payload.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize_url: %v", err)
	}
	if parsed.Host != "cloud.ouraring.com" {
		t.Fatalf("expected Oura auth host, got %s", parsed.Host)
	}
	if got := parsed.Query().Get("client_id"); got != "oura-client" {
		t.Fatalf("expected client_id in authorize URL, got %q", got)
	}
}

func TestOuraSyncPersistsRows(t *testing.T) {
	srv := newTestServer(t)
	srv.oura = oura.NewClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch {
			case req.URL.Path == "/v2/usercollection/daily_activity":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"activity-1","day":"2026-03-20","timestamp":"2026-03-20T23:59:59+00:00","steps":12345,"active_calories":450,"contributors":{"meet_daily_targets":90}}]}`), nil
			case req.URL.Path == "/v2/usercollection/daily_readiness":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"readiness-1","day":"2026-03-20","timestamp":"2026-03-20T07:00:00+00:00","score":82,"temperature_deviation":0.1}]}`), nil
			case req.URL.Path == "/v2/usercollection/sleep":
				return jsonResponse(http.StatusOK, `{"data":[{"id":"sleep-1","day":"2026-03-20","bedtime_start":"2026-03-19T23:00:00+00:00","bedtime_end":"2026-03-20T07:00:00+00:00","time_in_bed":28800,"total_sleep_duration":27000,"efficiency":88,"type":"long_sleep","deep_sleep_duration":5400,"light_sleep_duration":14400,"rem_sleep_duration":7200,"awake_time":1800,"average_heart_rate":55}]}`), nil
			default:
				return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
			}
		}),
	})

	saveJSON(t, srv, `{
		"user_timezone":"Europe/London",
		"providers":[
			{
				"provider":"fitbit",
				"client_id":"",
				"client_secret":"",
				"redirect_uri":"http://localhost:18080/oauth/fitbit/callback",
				"default_scopes":"activity heartrate sleep profile",
				"notes":"Fitbit notes"
			},
			{
				"provider":"oura",
				"client_id":"oura-client",
				"client_secret":"oura-secret",
				"redirect_uri":"http://localhost:18080/oauth/oura/callback",
				"default_scopes":"email personal daily",
				"notes":"Oura notes"
			}
		]
	}`)

	if err := srv.store.UpsertConnection(context.Background(), store.Connection{
		Provider:    "oura",
		AccessToken: "oura-access",
		Status:      "connected",
		ConnectedAt: "2026-03-20T08:00:00Z",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/oura/sync", strings.NewReader(`{"start_date":"2026-03-19","end_date":"2026-03-20"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Sync struct {
			Mode     string `json:"mode"`
			Entities []struct {
				Entity string `json:"entity"`
				Cursor string `json:"cursor"`
			} `json:"entities"`
		} `json:"sync"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal sync response: %v", err)
	}
	if payload.Sync.Mode != "backfill" {
		t.Fatalf("expected backfill mode, got %q", payload.Sync.Mode)
	}
	if len(payload.Sync.Entities) != 3 {
		t.Fatalf("expected 3 entity summaries, got %d", len(payload.Sync.Entities))
	}

	rows, err := srv.store.CanonicalExportRows(context.Background())
	if err != nil {
		t.Fatalf("canonical rows: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 canonical rows, got %d", len(rows))
	}

	for _, entity := range []string{"daily_activity", "daily_readiness", "sleep"} {
		cursor, _, err := srv.store.SyncState(context.Background(), "oura", entity)
		if err != nil {
			t.Fatalf("read sync state for %s: %v", entity, err)
		}
		if cursor != "2026-03-20" {
			t.Fatalf("expected cursor 2026-03-20 for %s, got %q", entity, cursor)
		}
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/v1/providers/oura/status", nil)
	statusRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 from status endpoint, got %d body=%s", statusRec.Code, statusRec.Body.String())
	}

	var statusPayload struct {
		SyncState []struct {
			EntityKind  string `json:"entity_kind"`
			CursorValue string `json:"cursor_value"`
		} `json:"sync_state"`
		LastSync struct {
			Mode      string `json:"mode"`
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		} `json:"last_sync"`
	}
	if err := json.Unmarshal(statusRec.Body.Bytes(), &statusPayload); err != nil {
		t.Fatalf("unmarshal status payload: %v", err)
	}
	if len(statusPayload.SyncState) != 3 {
		t.Fatalf("expected 3 sync_state entries, got %d", len(statusPayload.SyncState))
	}
	if statusPayload.LastSync.Mode != "backfill" {
		t.Fatalf("expected persisted last_sync mode backfill, got %q", statusPayload.LastSync.Mode)
	}
	if statusPayload.LastSync.EndDate != "2026-03-20" {
		t.Fatalf("expected persisted last_sync end_date 2026-03-20, got %q", statusPayload.LastSync.EndDate)
	}
}

func TestOuraSyncUsesCursorOverlapWhenStartDateOmitted(t *testing.T) {
	srv := newTestServer(t)
	requests := map[string][]string{}
	srv.oura = oura.NewClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rangeKey := req.URL.Query().Get("start_date") + ".." + req.URL.Query().Get("end_date")
			requests[req.URL.Path] = append(requests[req.URL.Path], rangeKey)
			return jsonResponse(http.StatusOK, `{"data":[]}`), nil
		}),
	})

	saveJSON(t, srv, `{
		"user_timezone":"Europe/London",
		"providers":[
			{
				"provider":"fitbit",
				"client_id":"",
				"client_secret":"",
				"redirect_uri":"http://localhost:18080/oauth/fitbit/callback",
				"default_scopes":"activity heartrate sleep profile",
				"notes":"Fitbit notes"
			},
			{
				"provider":"oura",
				"client_id":"oura-client",
				"client_secret":"oura-secret",
				"redirect_uri":"http://localhost:18080/oauth/oura/callback",
				"default_scopes":"email personal daily",
				"notes":"Oura notes"
			}
		]
	}`)

	if err := srv.store.UpsertConnection(context.Background(), store.Connection{
		Provider:    "oura",
		AccessToken: "oura-access",
		Status:      "connected",
		ConnectedAt: "2026-03-20T08:00:00Z",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	for _, entity := range []string{"daily_activity", "daily_readiness", "sleep"} {
		if err := srv.store.UpsertSyncState(context.Background(), "oura", entity, "2026-03-20", "2026-03-20T09:00:00Z"); err != nil {
			t.Fatalf("seed sync state for %s: %v", entity, err)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/providers/oura/sync", strings.NewReader(`{"end_date":"2026-03-24"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	for _, path := range []string{
		"/v2/usercollection/daily_activity",
		"/v2/usercollection/daily_readiness",
		"/v2/usercollection/sleep",
	} {
		ranges := requests[path]
		if len(ranges) != 1 {
			t.Fatalf("expected 1 request for %s, got %d", path, len(ranges))
		}
		if ranges[0] != "2026-03-17..2026-03-24" {
			t.Fatalf("expected overlap range for %s, got %q", path, ranges[0])
		}
	}
}

func TestOuraCallbackRedirectsBackToReturnTo(t *testing.T) {
	srv := newTestServer(t)
	srv.oura = oura.NewClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "api.ouraring.com" && req.URL.Path == "/oauth/token" {
				return jsonResponse(http.StatusOK, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600,"scope":"email personal daily"}`), nil
			}
			return jsonResponse(http.StatusNotFound, `{"error":"not found"}`), nil
		}),
	})

	saveJSON(t, srv, `{
		"user_timezone":"Europe/London",
		"providers":[
			{
				"provider":"fitbit",
				"client_id":"",
				"client_secret":"",
				"redirect_uri":"http://localhost:18080/oauth/fitbit/callback",
				"default_scopes":"activity heartrate sleep profile",
				"notes":"Fitbit notes"
			},
			{
				"provider":"oura",
				"client_id":"oura-client",
				"client_secret":"oura-secret",
				"redirect_uri":"http://localhost:18080/oauth/oura/callback",
				"default_scopes":"email personal daily",
				"notes":"Oura notes"
			}
		]
	}`)

	if err := srv.store.SetAppSetting(context.Background(), ouraOAuthStateKey, "state-123"); err != nil {
		t.Fatalf("set state: %v", err)
	}
	if err := srv.store.SetAppSetting(context.Background(), ouraOAuthReturnToKey, "http://localhost:5173/"); err != nil {
		t.Fatalf("set return_to: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/oauth/oura/callback?code=code-123&state=state-123", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected status 302, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "http://localhost:5173/?oauth_provider=oura&oauth_status=connected" {
		t.Fatalf("unexpected redirect location %q", got)
	}
}

func TestOuraRecentReturnsDailyRecordsAndSleepSessions(t *testing.T) {
	srv := newTestServer(t)

	if err := srv.store.UpsertDailyRecord(context.Background(), store.DailyRecord{
		Provider:     "oura",
		RecordKind:   "daily_activity",
		LocalDate:    "2026-03-21",
		ZoneOffset:   "+00:00",
		SourceDevice: "oura-ring",
		ExternalID:   "activity-21",
		Summary:      json.RawMessage(`{"steps":11111,"active_calories":333}`),
	}); err != nil {
		t.Fatalf("seed daily record: %v", err)
	}

	duration := 420
	efficiency := 91.2
	if err := srv.store.InsertSleepSession(context.Background(), store.SleepSession{
		Provider:          "oura",
		LocalDate:         "2026-03-21",
		ZoneOffset:        "+00:00",
		ExternalID:        "sleep-21",
		StartTime:         "2026-03-20T23:10:00+00:00",
		EndTime:           "2026-03-21T06:10:00+00:00",
		DurationMinutes:   &duration,
		EfficiencyPercent: &efficiency,
		Stages:            json.RawMessage(`{"deep_sleep_duration":5400}`),
		Metrics:           json.RawMessage(`{"average_heart_rate":54}`),
	}); err != nil {
		t.Fatalf("seed sleep session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/providers/oura/recent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		DailyRecords  []store.DailyRecord  `json:"daily_records"`
		SleepSessions []store.SleepSession `json:"sleep_sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(payload.DailyRecords) != 1 {
		t.Fatalf("expected 1 daily record, got %d", len(payload.DailyRecords))
	}
	if len(payload.SleepSessions) != 1 {
		t.Fatalf("expected 1 sleep session, got %d", len(payload.SleepSessions))
	}
}

func TestDashboardOverview(t *testing.T) {
	srv := newTestServer(t)

	if err := srv.store.UpsertDailyRecord(context.Background(), store.DailyRecord{
		Provider:     "oura",
		RecordKind:   "daily_activity",
		LocalDate:    "2026-03-20",
		ZoneOffset:   "+01:00",
		SourceDevice: "oura-ring-4",
		ExternalID:   "activity-1",
		Summary: json.RawMessage(`{
			"score": 69,
			"steps": 6480,
			"active_calories": 394,
			"total_calories": 2755,
			"equivalent_walking_distance": 6497,
			"high_activity_time": 0,
			"medium_activity_time": 1980,
			"low_activity_time": 16260,
			"resting_time": 21060,
			"non_wear_time": 240
		}`),
	}); err != nil {
		t.Fatalf("seed activity record: %v", err)
	}

	if err := srv.store.UpsertDailyRecord(context.Background(), store.DailyRecord{
		Provider:     "oura",
		RecordKind:   "daily_readiness",
		LocalDate:    "2026-03-20",
		ZoneOffset:   "+01:00",
		SourceDevice: "oura-ring-4",
		ExternalID:   "readiness-1",
		Summary:      json.RawMessage(`{"score":70,"temperature_deviation":0.17}`),
	}); err != nil {
		t.Fatalf("seed readiness record: %v", err)
	}

	duration := 438
	timeInBed := 462
	efficiency := 94.8
	if err := srv.store.InsertSleepSession(context.Background(), store.SleepSession{
		Provider:          "oura",
		LocalDate:         "2026-03-20",
		ZoneOffset:        "+01:00",
		ExternalID:        "sleep-1",
		StartTime:         "2026-03-19T22:58:00+01:00",
		EndTime:           "2026-03-20T06:16:00+01:00",
		DurationMinutes:   &duration,
		TimeInBedMinutes:  &timeInBed,
		EfficiencyPercent: &efficiency,
		Stages: json.RawMessage(`{
			"awake_time": 1920,
			"deep_sleep_duration": 4590,
			"light_sleep_duration": 9810,
			"rem_sleep_duration": 5640
		}`),
		Metrics: json.RawMessage(`{
			"average_heart_rate": 65.25,
			"average_hrv": 34,
			"type": "long_sleep"
		}`),
	}); err != nil {
		t.Fatalf("seed sleep session: %v", err)
	}

	napDuration := 20
	napInBed := 28
	napEfficiency := 71.0
	if err := srv.store.InsertSleepSession(context.Background(), store.SleepSession{
		Provider:          "oura",
		LocalDate:         "2026-03-20",
		ZoneOffset:        "+01:00",
		ExternalID:        "sleep-2",
		StartTime:         "2026-03-20T15:04:00+01:00",
		EndTime:           "2026-03-20T15:32:00+01:00",
		DurationMinutes:   &napDuration,
		TimeInBedMinutes:  &napInBed,
		EfficiencyPercent: &napEfficiency,
		IsNap:             true,
	}); err != nil {
		t.Fatalf("seed nap session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var payload struct {
		AvailableDays int      `json:"available_days"`
		Providers     []string `json:"providers"`
		Daily         []struct {
			Date     string `json:"date"`
			Activity struct {
				Score                 int `json:"score"`
				Steps                 int `json:"steps"`
				MediumActivityMinutes int `json:"medium_activity_minutes"`
				LowActivityMinutes    int `json:"low_activity_minutes"`
				RestingMinutes        int `json:"resting_minutes"`
			} `json:"activity"`
			Readiness struct {
				Score int `json:"score"`
			} `json:"readiness"`
			Sleep struct {
				DurationMinutes  int     `json:"duration_minutes"`
				AverageHeartRate float64 `json:"average_heart_rate"`
				DeepMinutes      int     `json:"deep_minutes"`
				NapsCount        int     `json:"naps_count"`
				NapMinutes       int     `json:"nap_minutes"`
				SleepType        string  `json:"sleep_type"`
			} `json:"sleep"`
		} `json:"daily"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal dashboard overview: %v", err)
	}

	if payload.AvailableDays != 1 {
		t.Fatalf("expected 1 available day, got %d", payload.AvailableDays)
	}
	if len(payload.Providers) != 1 || payload.Providers[0] != "oura" {
		t.Fatalf("unexpected providers: %+v", payload.Providers)
	}
	if len(payload.Daily) != 1 {
		t.Fatalf("expected 1 daily item, got %d", len(payload.Daily))
	}
	if payload.Daily[0].Activity.Score != 69 || payload.Daily[0].Activity.Steps != 6480 {
		t.Fatalf("unexpected activity summary: %+v", payload.Daily[0].Activity)
	}
	if payload.Daily[0].Activity.MediumActivityMinutes != 33 ||
		payload.Daily[0].Activity.LowActivityMinutes != 271 ||
		payload.Daily[0].Activity.RestingMinutes != 351 {
		t.Fatalf("unexpected activity minute conversions: %+v", payload.Daily[0].Activity)
	}
	if payload.Daily[0].Readiness.Score != 70 {
		t.Fatalf("unexpected readiness summary: %+v", payload.Daily[0].Readiness)
	}
	if payload.Daily[0].Sleep.DurationMinutes != 438 ||
		payload.Daily[0].Sleep.AverageHeartRate != 65.25 ||
		payload.Daily[0].Sleep.DeepMinutes != 76 ||
		payload.Daily[0].Sleep.NapsCount != 1 ||
		payload.Daily[0].Sleep.NapMinutes != 20 ||
		payload.Daily[0].Sleep.SleepType != "long_sleep" {
		t.Fatalf("unexpected sleep summary: %+v", payload.Daily[0].Sleep)
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()

	tempDir := t.TempDir()

	srv, err := New(config.Config{
		Host:       "localhost",
		Port:       18080,
		DataDir:    tempDir,
		DBPath:     tempDir + "/somascope.db",
		ConfigPath: tempDir + "/config.json",
		ExportsDir: tempDir + "/exports",
		RawDir:     tempDir + "/raw",
		LogsDir:    tempDir + "/logs",
		AuthMode:   config.AuthModeBYO,
	}, mustOpenStore(t, tempDir+"/somascope.db"), VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	return srv
}

func mustOpenStore(t *testing.T, dbPath string) *store.Store {
	t.Helper()

	store, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func saveJSON(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	return rec
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
