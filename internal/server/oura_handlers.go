package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/robince/somascope/internal/oura"
	"github.com/robince/somascope/internal/providersync"
	"github.com/robince/somascope/internal/store"
)

const (
	ouraOAuthStateKey    = "oauth:oura:state"
	ouraOAuthReturnToKey = "oauth:oura:return_to"
)

func (s *Server) handleOuraStatus(w http.ResponseWriter, r *http.Request) {
	provider, err := s.settings.Provider("oura")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	overview, err := s.store.ProviderOverview(r.Context(), "oura", provider.Configured)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	syncStates, err := s.store.SyncStatesByProvider(r.Context(), "oura")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	var currentRun *store.SyncRun
	if run, err := s.store.CurrentSyncRunByProvider(r.Context(), "oura"); err == nil {
		currentRun = &run
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	var lastCompletedRun *store.SyncRun
	if run, err := s.store.LatestFinishedSyncRunByProvider(r.Context(), "oura"); err == nil {
		lastCompletedRun = &run
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	var lastSuccessfulRun *store.SyncRun
	if run, err := s.store.LatestSuccessfulSyncRunByProvider(r.Context(), "oura"); err == nil {
		lastSuccessfulRun = &run
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	lastSuccessAt := overview.LastSyncAt
	if lastSuccessfulRun != nil && strings.TrimSpace(lastSuccessfulRun.FinishedAt) != "" {
		lastSuccessAt = lastSuccessfulRun.FinishedAt
	}

	lastActivityAt := lastSuccessAt
	if currentRun != nil && strings.TrimSpace(currentRun.UpdatedAt) != "" {
		lastActivityAt = currentRun.UpdatedAt
	} else if lastCompletedRun != nil && strings.TrimSpace(lastCompletedRun.UpdatedAt) != "" {
		lastActivityAt = lastCompletedRun.UpdatedAt
	}

	var lastError *store.SyncError
	switch {
	case currentRun != nil && currentRun.LastError != nil:
		lastError = currentRun.LastError
	case lastCompletedRun != nil && lastCompletedRun.LastError != nil:
		lastError = lastCompletedRun.LastError
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider":            overview.Provider,
		"configured":          overview.Configured,
		"connected":           overview.Connected,
		"status":              overview.Status,
		"scope":               overview.Scope,
		"connected_at":        overview.ConnectedAt,
		"token_expires_at":    overview.TokenExpiresAt,
		"last_sync_at":        lastSuccessAt,
		"last_success_at":     lastSuccessAt,
		"last_activity_at":    lastActivityAt,
		"daily_record_count":  overview.DailyRecordCount,
		"sleep_session_count": overview.SleepSessionCount,
		"sync_state":          syncStates,
		"current_run":         currentRun,
		"last_completed_run":  lastCompletedRun,
		"last_error":          lastError,
	})
}

func (s *Server) handleOuraRecent(w http.ResponseWriter, r *http.Request) {
	dailyRecords, err := s.store.RecentDailyRecords(r.Context(), "oura", 14)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	sleepSessions, err := s.store.RecentSleepSessions(r.Context(), "oura", 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"daily_records":  dailyRecords,
		"sleep_sessions": sleepSessions,
	})
}

func (s *Server) handleOuraAuthStart(w http.ResponseWriter, r *http.Request) {
	provider, err := s.settings.Provider("oura")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if strings.TrimSpace(provider.ClientID) == "" || strings.TrimSpace(provider.ClientSecret) == "" || strings.TrimSpace(provider.RedirectURI) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("oura settings are incomplete"))
		return
	}

	state, err := oauthState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.SetAppSetting(context.Background(), ouraOAuthStateKey, state); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	var payload struct {
		ReturnTo string `json:"return_to"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
			return
		}
	}

	returnTo := firstValidReturnTo(strings.TrimSpace(payload.ReturnTo), appRootFromRedirect(provider.RedirectURI))
	if returnTo != "" {
		if err := s.store.SetAppSetting(context.Background(), ouraOAuthReturnToKey, returnTo); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	authorizeURL, err := s.oura.AuthorizationURL(oura.AppConfig{
		ClientID:      provider.ClientID,
		ClientSecret:  provider.ClientSecret,
		RedirectURI:   provider.RedirectURI,
		DefaultScopes: provider.DefaultScopes,
	}, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authorize_url": authorizeURL,
	})
}

func (s *Server) handleOuraCallback(w http.ResponseWriter, r *http.Request) {
	if denied := r.URL.Query().Get("error"); denied != "" {
		writeOAuthHTML(w, "Oura authorization failed", template.HTMLEscapeString(denied))
		return
	}

	expectedState, err := s.store.AppSetting(r.Context(), ouraOAuthStateKey)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		http.Error(w, "failed to read oauth state", http.StatusInternalServerError)
		return
	}
	if expectedState == "" {
		http.Error(w, "no oauth flow in progress", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != expectedState {
		http.Error(w, "oauth state mismatch", http.StatusBadRequest)
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}

	provider, err := s.settings.Provider("oura")
	if err != nil {
		http.Error(w, "failed to load local Oura settings", http.StatusInternalServerError)
		return
	}

	bundle, err := s.oura.ExchangeCode(r.Context(), oura.AppConfig{
		ClientID:      provider.ClientID,
		ClientSecret:  provider.ClientSecret,
		RedirectURI:   provider.RedirectURI,
		DefaultScopes: provider.DefaultScopes,
	}, code)
	if err != nil {
		writeOAuthHTML(w, "Oura authorization failed", template.HTMLEscapeString(err.Error()))
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := s.store.UpsertConnection(r.Context(), store.Connection{
		Provider:       "oura",
		AccessToken:    bundle.AccessToken,
		RefreshToken:   bundle.RefreshToken,
		TokenExpiresAt: bundle.ExpiresAt.UTC().Format(time.RFC3339),
		Scope:          bundle.Scope,
		Status:         "connected",
		ConnectedAt:    now,
	}); err != nil {
		http.Error(w, "failed to save connection", http.StatusInternalServerError)
		return
	}

	_ = s.store.SetAppSetting(r.Context(), ouraOAuthStateKey, "")
	returnTo, _ := s.store.AppSetting(r.Context(), ouraOAuthReturnToKey)
	_ = s.store.SetAppSetting(r.Context(), ouraOAuthReturnToKey, "")
	returnTo = firstValidReturnTo(returnTo, appRootFromRedirect(provider.RedirectURI))
	if returnTo != "" {
		redirectURL := addQueryValues(returnTo, map[string]string{
			"oauth_provider": "oura",
			"oauth_status":   "connected",
		})
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	writeOAuthHTML(w, "Oura connected", "Authorization succeeded. You can return to somascope and run a sync.")
}

func (s *Server) handleOuraSync(w http.ResponseWriter, r *http.Request) {
	provider, err := s.settings.Provider("oura")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	connection, err := s.store.ConnectionByProvider(r.Context(), "oura")
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("oura is not connected yet"))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	var request struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil && err != io.EOF {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
			return
		}
	}

	endDate := strings.TrimSpace(request.EndDate)
	startDate := strings.TrimSpace(request.StartDate)
	mode := "incremental"
	if startDate != "" {
		mode = "backfill"
	}

	run, alreadyRunning, err := s.syncs.Start("oura", mode, startDate, endDate, func(ctx context.Context, tracker *providersync.Tracker) error {
		return oura.Sync(ctx, s.store, s.oura, oura.AppConfig{
			ClientID:      provider.ClientID,
			ClientSecret:  provider.ClientSecret,
			RedirectURI:   provider.RedirectURI,
			DefaultScopes: provider.DefaultScopes,
		}, connection, oura.SyncOptions{
			StartDate: startDate,
			EndDate:   endDate,
			Tracker:   tracker,
		})
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if alreadyRunning {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       "Oura sync is already running.",
			"current_run": run,
		})
		return
	}

	overview, err := s.store.ProviderOverview(r.Context(), "oura", provider.Configured)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":       true,
		"started":  true,
		"run":      run,
		"overview": overview,
	})
}

func oauthState() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func writeOAuthHTML(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>%s</title><style>body{font-family:ui-sans-serif,system-ui,sans-serif;background:#f4f0e6;color:#203025;padding:40px}main{max-width:640px;margin:0 auto;border:1px solid #d8d1bf;border-radius:18px;padding:24px;background:#fffdf7}h1{margin:0 0 12px;font-size:1.5rem}p{line-height:1.6;color:#455348}</style></head><body><main><h1>%s</h1><p>%s</p></main></body></html>`,
		template.HTMLEscapeString(title),
		template.HTMLEscapeString(title),
		body,
	)
}

func appRootFromRedirect(redirectURI string) string {
	parsed, err := url.Parse(strings.TrimSpace(redirectURI))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Path = "/"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func firstValidReturnTo(values ...string) string {
	for _, value := range values {
		if validated := validateLocalReturnTo(value); validated != "" {
			return validated
		}
	}
	return ""
}

func validateLocalReturnTo(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "http" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" {
		return ""
	}
	return parsed.String()
}

func addQueryValues(rawURL string, values map[string]string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	for key, value := range values {
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
