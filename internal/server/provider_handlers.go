package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/robince/somascope/internal/googlehealth"
	"github.com/robince/somascope/internal/oura"
	"github.com/robince/somascope/internal/providersync"
	"github.com/robince/somascope/internal/settings"
	"github.com/robince/somascope/internal/store"
)

func (s *Server) handleProviderStatus(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if !isKnownProvider(provider) {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown provider %q", provider))
		return
	}

	payload, err := s.providerStatus(r.Context(), provider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleProviderRecent(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if !isKnownProvider(provider) {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown provider %q", provider))
		return
	}

	dailyRecords, err := s.store.RecentDailyRecords(r.Context(), provider, 14)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	sleepSessions, err := s.store.RecentSleepSessions(r.Context(), provider, 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"daily_records":  dailyRecords,
		"sleep_sessions": sleepSessions,
	})
}

func (s *Server) handleProviderAuthStart(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if !isKnownProvider(provider) {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown provider %q", provider))
		return
	}

	cfg, err := s.settings.Provider(provider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" || strings.TrimSpace(cfg.RedirectURI) == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("%s settings are incomplete", providerDisplayName(provider)))
		return
	}

	state, err := oauthState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.store.SetAppSetting(context.Background(), oauthStateKey(provider), state); err != nil {
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

	returnTo := firstValidReturnTo(strings.TrimSpace(payload.ReturnTo), appRootFromRedirect(cfg.RedirectURI))
	if returnTo != "" {
		if err := s.store.SetAppSetting(context.Background(), oauthReturnToKey(provider), returnTo); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}

	authorizeURL, err := s.authorizationURL(provider, cfg, state)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authorize_url": authorizeURL,
	})
}

func (s *Server) handleProviderCallback(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if !isKnownProvider(provider) {
		http.Error(w, "unknown provider", http.StatusNotFound)
		return
	}

	if denied := r.URL.Query().Get("error"); denied != "" {
		writeOAuthHTML(w, providerDisplayName(provider)+" authorization failed", templateEscape(denied))
		return
	}

	expectedState, err := s.store.AppSetting(r.Context(), oauthStateKey(provider))
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

	cfg, err := s.settings.Provider(provider)
	if err != nil {
		http.Error(w, "failed to load local provider settings", http.StatusInternalServerError)
		return
	}

	connection, err := s.completeProviderAuth(r.Context(), provider, cfg, code)
	if err != nil {
		writeOAuthHTML(w, providerDisplayName(provider)+" authorization failed", templateEscape(err.Error()))
		return
	}

	if err := s.store.UpsertConnection(r.Context(), connection); err != nil {
		http.Error(w, "failed to save connection", http.StatusInternalServerError)
		return
	}

	_ = s.store.SetAppSetting(r.Context(), oauthStateKey(provider), "")
	_ = s.store.SetAppSetting(r.Context(), oauthVerifierKey(provider), "")
	returnTo, _ := s.store.AppSetting(r.Context(), oauthReturnToKey(provider))
	_ = s.store.SetAppSetting(r.Context(), oauthReturnToKey(provider), "")
	returnTo = firstValidReturnTo(returnTo, appRootFromRedirect(cfg.RedirectURI))
	if returnTo != "" {
		redirectURL := addQueryValues(returnTo, map[string]string{
			"oauth_provider": provider,
			"oauth_status":   "connected",
		})
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	writeOAuthHTML(w, providerDisplayName(provider)+" connected", "Authorization succeeded. You can return to somascope and run a sync.")
}

func (s *Server) handleProviderSync(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if !isKnownProvider(provider) {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown provider %q", provider))
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

	result, err := s.startProviderSync(r.Context(), provider, strings.TrimSpace(request.StartDate), strings.TrimSpace(request.EndDate))
	if err != nil {
		writeError(w, statusForSyncError(err), err)
		return
	}
	if result.AlreadyRunning {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":       providerDisplayName(provider) + " sync is already running.",
			"current_run": result.Run,
		})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":       true,
		"started":  true,
		"run":      result.Run,
		"overview": result.Overview,
	})
}

func (s *Server) handleSyncAll(w http.ResponseWriter, r *http.Request) {
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

	startDate := strings.TrimSpace(request.StartDate)
	endDate := strings.TrimSpace(request.EndDate)

	var started []map[string]any
	var alreadyRunning []map[string]any
	var skipped []map[string]any

	for _, provider := range knownProviders() {
		result, err := s.startProviderSync(r.Context(), provider, startDate, endDate)
		if errors.Is(err, errProviderNotConnected) {
			skipped = append(skipped, map[string]any{
				"provider": provider,
				"reason":   "not_connected",
			})
			continue
		}
		if err != nil {
			writeError(w, statusForSyncError(err), err)
			return
		}
		item := map[string]any{
			"provider": provider,
			"run":      result.Run,
		}
		if result.AlreadyRunning {
			alreadyRunning = append(alreadyRunning, item)
			continue
		}
		started = append(started, item)
	}

	if len(started) == 0 && len(alreadyRunning) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":           "All connected providers are already syncing.",
			"already_running": alreadyRunning,
			"skipped":         skipped,
		})
		return
	}

	status := http.StatusAccepted
	if len(started) == 0 && len(alreadyRunning) == 0 {
		status = http.StatusBadRequest
		writeJSON(w, status, map[string]any{
			"ok":      false,
			"error":   "No connected providers to sync.",
			"skipped": skipped,
		})
		return
	}

	writeJSON(w, status, map[string]any{
		"ok":              true,
		"started":         started,
		"already_running": alreadyRunning,
		"skipped":         skipped,
	})
}

var errProviderNotConnected = errors.New("provider is not connected")

type providerSyncStart struct {
	Run            store.SyncRun
	Overview       store.ProviderOverview
	AlreadyRunning bool
}

func (s *Server) startProviderSync(ctx context.Context, provider, startDate, endDate string) (providerSyncStart, error) {
	cfg, err := s.settings.Provider(provider)
	if err != nil {
		return providerSyncStart{}, err
	}
	connection, err := s.store.ConnectionByProvider(ctx, provider)
	if errors.Is(err, store.ErrNotFound) {
		return providerSyncStart{}, errProviderNotConnected
	}
	if err != nil {
		return providerSyncStart{}, err
	}
	if connection.Status != "connected" || strings.TrimSpace(connection.AccessToken) == "" {
		return providerSyncStart{}, errProviderNotConnected
	}

	mode := "incremental"
	if startDate != "" {
		mode = "backfill"
	}

	task, err := s.syncTask(provider, cfg, connection, startDate, endDate)
	if err != nil {
		return providerSyncStart{}, err
	}

	run, alreadyRunning, err := s.syncs.Start(provider, mode, startDate, endDate, task)
	if err != nil {
		return providerSyncStart{}, err
	}

	overview, err := s.store.ProviderOverview(ctx, provider, cfg.Configured)
	if err != nil {
		return providerSyncStart{}, err
	}

	return providerSyncStart{
		Run:            run,
		Overview:       overview,
		AlreadyRunning: alreadyRunning,
	}, nil
}

func (s *Server) syncTask(provider string, cfg settings.ProviderConfig, connection store.Connection, startDate, endDate string) (providersync.Task, error) {
	switch provider {
	case providerOura:
		return func(ctx context.Context, tracker *providersync.Tracker) error {
			return oura.Sync(ctx, s.store, s.oura, oura.AppConfig{
				ClientID:      cfg.ClientID,
				ClientSecret:  cfg.ClientSecret,
				RedirectURI:   cfg.RedirectURI,
				DefaultScopes: cfg.DefaultScopes,
			}, connection, oura.SyncOptions{
				StartDate: startDate,
				EndDate:   endDate,
				Tracker:   tracker,
			})
		}, nil
	case providerGoogleHealth:
		return func(ctx context.Context, tracker *providersync.Tracker) error {
			return googlehealth.Sync(ctx, s.store, s.googleHealth, googlehealth.AppConfig{
				ClientID:      cfg.ClientID,
				ClientSecret:  cfg.ClientSecret,
				RedirectURI:   cfg.RedirectURI,
				DefaultScopes: cfg.DefaultScopes,
			}, connection, googlehealth.SyncOptions{
				StartDate: startDate,
				EndDate:   endDate,
				Tracker:   tracker,
			})
		}, nil
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
}

func (s *Server) authorizationURL(provider string, cfg settings.ProviderConfig, state string) (string, error) {
	switch provider {
	case providerOura:
		return s.oura.AuthorizationURL(oura.AppConfig{
			ClientID:      cfg.ClientID,
			ClientSecret:  cfg.ClientSecret,
			RedirectURI:   cfg.RedirectURI,
			DefaultScopes: cfg.DefaultScopes,
		}, state)
	case providerGoogleHealth:
		verifier, challenge, err := googlehealth.NewPKCE()
		if err != nil {
			return "", err
		}
		if err := s.store.SetAppSetting(context.Background(), oauthVerifierKey(provider), verifier); err != nil {
			return "", err
		}
		return s.googleHealth.AuthorizationURL(googlehealth.AppConfig{
			ClientID:      cfg.ClientID,
			ClientSecret:  cfg.ClientSecret,
			RedirectURI:   cfg.RedirectURI,
			DefaultScopes: cfg.DefaultScopes,
		}, state, challenge)
	default:
		return "", fmt.Errorf("unknown provider %q", provider)
	}
}

func (s *Server) completeProviderAuth(ctx context.Context, provider string, cfg settings.ProviderConfig, code string) (store.Connection, error) {
	now := nowRFC3339()
	switch provider {
	case providerOura:
		bundle, err := s.oura.ExchangeCode(ctx, oura.AppConfig{
			ClientID:      cfg.ClientID,
			ClientSecret:  cfg.ClientSecret,
			RedirectURI:   cfg.RedirectURI,
			DefaultScopes: cfg.DefaultScopes,
		}, code)
		if err != nil {
			return store.Connection{}, err
		}
		expiresAt := ""
		if !bundle.ExpiresAt.IsZero() {
			expiresAt = bundle.ExpiresAt.UTC().Format(time.RFC3339)
		}
		return store.Connection{
			Provider:       providerOura,
			AccessToken:    bundle.AccessToken,
			RefreshToken:   bundle.RefreshToken,
			TokenExpiresAt: expiresAt,
			Scope:          bundle.Scope,
			Status:         "connected",
			ConnectedAt:    now,
		}, nil
	case providerGoogleHealth:
		verifier, _ := s.store.AppSetting(ctx, oauthVerifierKey(provider))
		bundle, err := s.googleHealth.ExchangeCode(ctx, googlehealth.AppConfig{
			ClientID:      cfg.ClientID,
			ClientSecret:  cfg.ClientSecret,
			RedirectURI:   cfg.RedirectURI,
			DefaultScopes: cfg.DefaultScopes,
		}, code, verifier)
		if err != nil {
			return store.Connection{}, err
		}
		identity, err := s.googleHealth.GetIdentity(ctx, bundle.AccessToken)
		if err != nil {
			return store.Connection{}, err
		}
		expiresAt := ""
		if !bundle.ExpiresAt.IsZero() {
			expiresAt = bundle.ExpiresAt.UTC().Format(time.RFC3339)
		}
		return store.Connection{
			Provider:          providerGoogleHealth,
			ExternalAccountID: identity.HealthUserID,
			AccessToken:       bundle.AccessToken,
			RefreshToken:      bundle.RefreshToken,
			TokenExpiresAt:    expiresAt,
			Scope:             bundle.Scope,
			Status:            "connected",
			ConnectedAt:       now,
		}, nil
	default:
		return store.Connection{}, fmt.Errorf("unknown provider %q", provider)
	}
}

func (s *Server) providerStatus(ctx context.Context, provider string) (map[string]any, error) {
	cfg, err := s.settings.Provider(provider)
	if err != nil {
		return nil, err
	}

	overview, err := s.store.ProviderOverview(ctx, provider, cfg.Configured)
	if err != nil {
		return nil, err
	}

	syncStates, err := s.store.SyncStatesByProvider(ctx, provider)
	if err != nil {
		return nil, err
	}

	var currentRun *store.SyncRun
	if run, err := s.store.CurrentSyncRunByProvider(ctx, provider); err == nil {
		currentRun = &run
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	var lastCompletedRun *store.SyncRun
	if run, err := s.store.LatestFinishedSyncRunByProvider(ctx, provider); err == nil {
		lastCompletedRun = &run
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	var lastSuccessfulRun *store.SyncRun
	if run, err := s.store.LatestSuccessfulSyncRunByProvider(ctx, provider); err == nil {
		lastSuccessfulRun = &run
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
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
	case currentRun == nil && lastCompletedRun != nil && lastCompletedRun.LastError != nil:
		lastError = lastCompletedRun.LastError
	}

	return map[string]any{
		"provider":            overview.Provider,
		"display_name":        providerDisplayName(provider),
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
	}, nil
}

func (s *Server) handleOuraStatus(w http.ResponseWriter, r *http.Request) {
	r.SetPathValue("provider", providerOura)
	s.handleProviderStatus(w, r)
}

func (s *Server) handleOuraRecent(w http.ResponseWriter, r *http.Request) {
	r.SetPathValue("provider", providerOura)
	s.handleProviderRecent(w, r)
}

func (s *Server) handleOuraAuthStart(w http.ResponseWriter, r *http.Request) {
	r.SetPathValue("provider", providerOura)
	s.handleProviderAuthStart(w, r)
}

func (s *Server) handleOuraCallback(w http.ResponseWriter, r *http.Request) {
	r.SetPathValue("provider", providerOura)
	s.handleProviderCallback(w, r)
}

func (s *Server) handleOuraSync(w http.ResponseWriter, r *http.Request) {
	r.SetPathValue("provider", providerOura)
	s.handleProviderSync(w, r)
}

func oauthStateKey(provider string) string {
	return "oauth:" + provider + ":state"
}

func oauthReturnToKey(provider string) string {
	return "oauth:" + provider + ":return_to"
}

func oauthVerifierKey(provider string) string {
	return "oauth:" + provider + ":code_verifier"
}

func statusForSyncError(err error) int {
	if errors.Is(err, errProviderNotConnected) {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func templateEscape(value string) string {
	return template.HTMLEscapeString(value)
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
