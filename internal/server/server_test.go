package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/robince/somascope/internal/config"
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
	}, VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	return srv
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
