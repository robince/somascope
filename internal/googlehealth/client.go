package googlehealth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	TokenURL     = "https://oauth2.googleapis.com/token"
	APIBaseURL   = "https://health.googleapis.com/v4"
	Provider     = "google_health"
	maxPageSize  = 10000
	maxPages     = 500
)

func DefaultReadonlyScopes() []string {
	return []string{
		"https://www.googleapis.com/auth/googlehealth.activity_and_fitness.readonly",
		"https://www.googleapis.com/auth/googlehealth.sleep.readonly",
		"https://www.googleapis.com/auth/googlehealth.health_metrics_and_measurements.readonly",
		"https://www.googleapis.com/auth/googlehealth.nutrition.readonly",
		"https://www.googleapis.com/auth/googlehealth.profile.readonly",
		"https://www.googleapis.com/auth/googlehealth.settings.readonly",
		"https://www.googleapis.com/auth/googlehealth.ecg.readonly",
		"https://www.googleapis.com/auth/googlehealth.irn.readonly",
		"https://www.googleapis.com/auth/googlehealth.location.readonly",
	}
}

func DefaultReadonlyScopeString() string {
	return strings.Join(DefaultReadonlyScopes(), " ")
}

type AppConfig struct {
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	DefaultScopes string
}

type TokenBundle struct {
	AccessToken  string
	RefreshToken string
	Scope        string
	ExpiresAt    time.Time
}

type Identity struct {
	HealthUserID string
	LegacyUserID string
}

type Client struct {
	HTTPClient  *http.Client
	RateLimiter RequestLimiter
}

type RequestLimiter interface {
	Wait(context.Context) error
}

type RequestPacer struct {
	interval time.Duration
	mu       sync.Mutex
	nextAt   time.Time
}

type RetryConfig struct {
	MaxAttempts    int
	OnRetry        func(*APIError, time.Duration)
	OnUnauthorized func(ctx context.Context, staleToken string) (string, error)
}

type APIError struct {
	Method       string
	Path         string
	Attempt      int
	StatusCode   int
	ResponseBody string
	RetryAfter   time.Duration
	Err          error
}

type ListPage struct {
	DataPoints    []map[string]any
	NextPageToken string
	RawBody       json.RawMessage
}

type DailyRollupPage struct {
	Points  []map[string]any
	RawBody json.RawMessage
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{HTTPClient: httpClient}
}

func (c *Client) WithRateLimiter(limiter RequestLimiter) *Client {
	if c == nil {
		return nil
	}
	return &Client{
		HTTPClient:  c.HTTPClient,
		RateLimiter: limiter,
	}
}

func NewRequestPacer(maxRequests int, per time.Duration) *RequestPacer {
	if maxRequests < 1 {
		maxRequests = 1
	}
	if per <= 0 {
		per = time.Second
	}
	interval := time.Duration(math.Ceil(float64(per) / float64(maxRequests)))
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	return &RequestPacer{interval: interval}
}

func (p *RequestPacer) Wait(ctx context.Context) error {
	if p == nil || p.interval <= 0 {
		return nil
	}
	p.mu.Lock()
	now := time.Now()
	readyAt := now
	if p.nextAt.After(now) {
		readyAt = p.nextAt
	}
	p.nextAt = readyAt.Add(p.interval)
	p.mu.Unlock()

	delay := time.Until(readyAt)
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func NewPKCE() (verifier, challenge string, err error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buffer)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

func (c *Client) AuthorizationURL(cfg AppConfig, state, codeChallenge string) (string, error) {
	if strings.TrimSpace(cfg.ClientID) == "" {
		return "", fmt.Errorf("google health client_id is required")
	}
	if strings.TrimSpace(cfg.RedirectURI) == "" {
		return "", fmt.Errorf("google health redirect_uri is required")
	}
	if strings.TrimSpace(state) == "" {
		return "", fmt.Errorf("oauth state is required")
	}
	if strings.TrimSpace(codeChallenge) == "" {
		return "", fmt.Errorf("pkce code challenge is required")
	}

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", cfg.ClientID)
	values.Set("redirect_uri", cfg.RedirectURI)
	values.Set("state", state)
	values.Set("access_type", "offline")
	values.Set("prompt", "consent")
	values.Set("include_granted_scopes", "true")
	values.Set("code_challenge", codeChallenge)
	values.Set("code_challenge_method", "S256")
	values.Set("scope", firstNonEmpty(strings.TrimSpace(cfg.DefaultScopes), defaultScopes()))
	return AuthorizeURL + "?" + values.Encode(), nil
}

func (c *Client) ExchangeCode(ctx context.Context, cfg AppConfig, code, verifier string) (TokenBundle, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", cfg.RedirectURI)
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	if strings.TrimSpace(verifier) != "" {
		values.Set("code_verifier", verifier)
	}
	return c.tokenRequest(ctx, values)
}

func (c *Client) RefreshToken(ctx context.Context, cfg AppConfig, refreshToken string) (TokenBundle, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
	return c.tokenRequest(ctx, values)
}

func (c *Client) tokenRequest(ctx context.Context, values url.Values) (TokenBundle, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return TokenBundle{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return TokenBundle{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return TokenBundle{}, err
	}
	if resp.StatusCode >= 400 {
		return TokenBundle{}, fmt.Errorf("google token request failed: %s", truncate(strings.TrimSpace(string(body)), 512))
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return TokenBundle{}, err
	}

	expiresAt := time.Time{}
	if payload.ExpiresIn > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return TokenBundle{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		Scope:        payload.Scope,
		ExpiresAt:    expiresAt,
	}, nil
}

func (c *Client) GetRaw(ctx context.Context, accessToken, path string, retry RetryConfig) (json.RawMessage, error) {
	return c.doJSON(ctx, http.MethodGet, accessToken, path, nil, nil, nil, retry)
}

func (c *Client) GetIdentity(ctx context.Context, accessToken string) (Identity, error) {
	var payload struct {
		LegacyUserID string `json:"legacyUserId"`
		HealthUserID string `json:"healthUserId"`
	}
	if _, err := c.doJSON(ctx, http.MethodGet, accessToken, "/users/me/identity", nil, nil, &payload, RetryConfig{MaxAttempts: 2}); err != nil {
		return Identity{}, err
	}
	return Identity{
		HealthUserID: payload.HealthUserID,
		LegacyUserID: payload.LegacyUserID,
	}, nil
}

func (c *Client) DailyRollup(ctx context.Context, accessToken, dataType string, start, end time.Time, retry RetryConfig) (DailyRollupPage, error) {
	body := map[string]any{
		"range": map[string]any{
			"start": civilDateTime(start, 0, 0, 0),
			"end":   civilDateTime(end, 23, 59, 59),
		},
		"windowSizeDays": 1,
	}
	var payload struct {
		RollupDataPoints []map[string]any `json:"rollupDataPoints"`
	}
	raw, err := c.doJSON(ctx, http.MethodPost, accessToken, "/users/me/dataTypes/"+dataType+"/dataPoints:dailyRollUp", nil, body, &payload, retry)
	if err != nil {
		return DailyRollupPage{}, err
	}
	return DailyRollupPage{Points: payload.RollupDataPoints, RawBody: raw}, nil
}

func (c *Client) ListDataPoints(ctx context.Context, accessToken, dataType string, params url.Values, retry RetryConfig) ([]ListPage, error) {
	return c.listPaged(ctx, accessToken, "/users/me/dataTypes/"+dataType+"/dataPoints", params, retry)
}

func (c *Client) ReconcileDataPoints(ctx context.Context, accessToken, dataType string, params url.Values, retry RetryConfig) ([]ListPage, error) {
	return c.listPaged(ctx, accessToken, "/users/me/dataTypes/"+dataType+"/dataPoints:reconcile", params, retry)
}

func (c *Client) listPaged(ctx context.Context, accessToken, path string, params url.Values, retry RetryConfig) ([]ListPage, error) {
	var out []ListPage
	seen := map[string]struct{}{}
	pageToken := ""
	for page := 0; page < maxPages; page++ {
		pageParams := withPageSize(params)
		if pageToken != "" {
			pageParams.Set("pageToken", pageToken)
		}
		var payload struct {
			DataPoints    []map[string]any `json:"dataPoints"`
			NextPageToken string           `json:"nextPageToken"`
		}
		raw, err := c.doJSON(ctx, http.MethodGet, accessToken, path, pageParams, nil, &payload, retry)
		if err != nil {
			return nil, err
		}
		next := strings.TrimSpace(payload.NextPageToken)
		out = append(out, ListPage{
			DataPoints:    payload.DataPoints,
			NextPageToken: next,
			RawBody:       raw,
		})
		if next == "" {
			return out, nil
		}
		if _, ok := seen[next]; ok {
			return nil, fmt.Errorf("google health api %s returned a repeated page token", path)
		}
		seen[next] = struct{}{}
		pageToken = next
	}
	return nil, fmt.Errorf("google health api %s exceeded %d pages", path, maxPages)
}

func (c *Client) doJSON(ctx context.Context, method, accessToken, path string, params url.Values, body any, target any, retry RetryConfig) (json.RawMessage, error) {
	maxAttempts := retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	currentToken := accessToken
	refreshed := false
	var encodedBody []byte
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		encodedBody = raw
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		reqURL := APIBaseURL + path
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}

		var reader io.Reader
		if encodedBody != nil {
			reader = strings.NewReader(string(encodedBody))
		}
		req, err := http.NewRequestWithContext(ctx, method, reqURL, reader)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+currentToken)
		req.Header.Set("Accept", "application/json")
		if encodedBody != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		if c.RateLimiter != nil {
			if err := c.RateLimiter.Wait(ctx); err != nil {
				return nil, err
			}
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			apiErr := &APIError{Method: method, Path: path, Attempt: attempt, Err: err}
			if apiErr.Retriable() && attempt < maxAttempts {
				backoff := retryDelay(attempt, 0)
				if retry.OnRetry != nil {
					retry.OnRetry(apiErr, backoff)
				}
				if sleepErr := sleepWithContext(ctx, backoff); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, apiErr
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode >= 400 {
			apiErr := &APIError{
				Method:       method,
				Path:         path,
				Attempt:      attempt,
				StatusCode:   resp.StatusCode,
				ResponseBody: truncate(strings.TrimSpace(string(respBody)), 512),
				RetryAfter:   parseRetryAfter(resp.Header.Get("Retry-After")),
			}
			if resp.StatusCode == http.StatusUnauthorized && !refreshed && retry.OnUnauthorized != nil {
				newToken, refreshErr := retry.OnUnauthorized(ctx, currentToken)
				if refreshErr == nil && newToken != "" {
					currentToken = newToken
					refreshed = true
					attempt--
					continue
				}
			}
			if apiErr.Retriable() && attempt < maxAttempts {
				backoff := retryDelay(attempt, apiErr.RetryAfter)
				if retry.OnRetry != nil {
					retry.OnRetry(apiErr, backoff)
				}
				if sleepErr := sleepWithContext(ctx, backoff); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, apiErr
		}

		if target != nil {
			if err := json.Unmarshal(respBody, target); err != nil {
				return nil, err
			}
		}
		return json.RawMessage(respBody), nil
	}

	return nil, fmt.Errorf("google health api %s %s exceeded retry budget", method, path)
}

func (e *APIError) Error() string {
	switch {
	case e.Err != nil:
		return fmt.Sprintf("google health api %s %s request failed: %v", e.Method, e.Path, e.Err)
	case e.StatusCode > 0:
		return fmt.Sprintf("google health api %s %s failed with status %d: %s", e.Method, e.Path, e.StatusCode, e.ResponseBody)
	default:
		return fmt.Sprintf("google health api %s %s failed", e.Method, e.Path)
	}
}

func (e *APIError) Retriable() bool {
	if e.Err != nil {
		return true
	}
	return e.StatusCode == http.StatusTooManyRequests ||
		e.StatusCode == http.StatusRequestTimeout ||
		e.StatusCode >= http.StatusInternalServerError
}

func withPageSize(params url.Values) url.Values {
	pageParams := url.Values{}
	for key, values := range params {
		for _, value := range values {
			pageParams.Add(key, value)
		}
	}
	if strings.TrimSpace(pageParams.Get("pageSize")) == "" {
		pageParams.Set("pageSize", strconv.Itoa(maxPageSize))
	}
	return pageParams
}

func defaultScopes() string {
	return DefaultReadonlyScopeString()
}

func civilDateTime(day time.Time, hour, minute, second int) map[string]any {
	return map[string]any{
		"date": map[string]any{
			"year":  day.Year(),
			"month": int(day.Month()),
			"day":   day.Day(),
		},
		"time": map[string]any{
			"hours":   hour,
			"minutes": minute,
			"seconds": second,
		},
	}
}

func parseRetryAfter(value string) time.Duration {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		return time.Until(when)
	}
	return 0
}

func retryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	base := time.Duration(1<<maxInt(attempt-1, 0)) * time.Second
	if base > 15*time.Second {
		base = 15 * time.Second
	}
	return base + time.Duration(attempt*150)*time.Millisecond
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
