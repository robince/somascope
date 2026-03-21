package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/robince/somascope/internal/config"
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
				"redirect_uri":"http://127.0.0.1:8080/oauth/fitbit/callback",
				"default_scopes":"activity heartrate sleep profile",
				"notes":"Fitbit notes"
			},
			{
				"provider":"oura",
				"client_id":"",
				"client_secret":"",
				"redirect_uri":"http://127.0.0.1:8080/oauth/oura/callback",
				"default_scopes":"daily heartrate personal email",
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
				"redirect_uri":"http://127.0.0.1:8080/oauth/fitbit/callback",
				"default_scopes":"activity heartrate sleep profile",
				"notes":"Fitbit notes"
			},
			{
				"provider":"oura",
				"client_id":"",
				"client_secret":"",
				"redirect_uri":"http://127.0.0.1:8080/oauth/oura/callback",
				"default_scopes":"daily heartrate personal email",
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

func newTestServer(t *testing.T) *Server {
	t.Helper()

	tempDir := t.TempDir()

	srv, err := New(config.Config{
		Host:       "127.0.0.1",
		Port:       8080,
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
