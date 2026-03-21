package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func newTestServer(t *testing.T) *Server {
	t.Helper()

	srv, err := New(config.Config{
		Host:       "127.0.0.1",
		Port:       8080,
		DataDir:    "/tmp/somascope-test",
		DBPath:     "/tmp/somascope-test/somascope.db",
		ConfigPath: "/tmp/somascope-test/config.json",
		ExportsDir: "/tmp/somascope-test/exports",
		RawDir:     "/tmp/somascope-test/raw",
		LogsDir:    "/tmp/somascope-test/logs",
		AuthMode:   config.AuthModeBYO,
	}, VersionInfo{Version: "test"})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	return srv
}
