package oura

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	AuthorizeURL = "https://cloud.ouraring.com/oauth/authorize"
	TokenURL     = "https://api.ouraring.com/oauth/token"
	APIBaseURL   = "https://api.ouraring.com"
)

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

type Client struct {
	HTTPClient  *http.Client
	RateLimiter RequestLimiter
}

type RetryConfig struct {
	MaxAttempts    int
	OnRetry        func(*APIError, time.Duration)
	OnUnauthorized func(ctx context.Context, staleToken string) (string, error)
}

type CollectionPage struct {
	Query     url.Values
	Data      []map[string]any
	NextToken string
	RawBody   json.RawMessage
}

type DocumentResult struct {
	Value   map[string]any
	RawBody json.RawMessage
}

type APIError struct {
	Path         string
	Query        url.Values
	Attempt      int
	StatusCode   int
	ResponseBody string
	RetryAfter   time.Duration
	Err          error
}

type RequestLimiter interface {
	Wait(context.Context) error
}

type RequestPacer struct {
	interval time.Duration
	mu       sync.Mutex
	nextAt   time.Time
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

func (e *APIError) Error() string {
	switch {
	case e.Err != nil:
		return fmt.Sprintf("oura api %s request failed: %v", e.Path, e.Err)
	case e.StatusCode > 0:
		return fmt.Sprintf("oura api %s failed with status %d: %s", e.Path, e.StatusCode, e.ResponseBody)
	default:
		return fmt.Sprintf("oura api %s failed", e.Path)
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

func (c *Client) AuthorizationURL(cfg AppConfig, state string) (string, error) {
	if strings.TrimSpace(cfg.ClientID) == "" {
		return "", fmt.Errorf("oura client_id is required")
	}
	if strings.TrimSpace(cfg.RedirectURI) == "" {
		return "", fmt.Errorf("oura redirect_uri is required")
	}

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", cfg.ClientID)
	values.Set("redirect_uri", cfg.RedirectURI)
	values.Set("state", state)
	if scope := strings.TrimSpace(cfg.DefaultScopes); scope != "" {
		values.Set("scope", scope)
	}
	return AuthorizeURL + "?" + values.Encode(), nil
}

func (c *Client) ExchangeCode(ctx context.Context, cfg AppConfig, code string) (TokenBundle, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", cfg.RedirectURI)
	values.Set("client_id", cfg.ClientID)
	values.Set("client_secret", cfg.ClientSecret)
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
		return TokenBundle{}, fmt.Errorf("oura token request failed: %s", strings.TrimSpace(string(body)))
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

func (c *Client) FetchCollection(ctx context.Context, accessToken, path string, params url.Values, retry RetryConfig) ([]map[string]any, error) {
	pages, err := c.FetchCollectionPages(ctx, accessToken, path, params, retry)
	if err != nil {
		return nil, err
	}

	var out []map[string]any
	for _, page := range pages {
		out = append(out, page.Data...)
	}
	return out, nil
}

func (c *Client) FetchCollectionPages(ctx context.Context, accessToken, path string, params url.Values, retry RetryConfig) ([]CollectionPage, error) {
	var out []CollectionPage
	var nextToken string
	for {
		pageParams := url.Values{}
		maps.Copy(pageParams, params)
		if nextToken != "" {
			pageParams.Set("next_token", nextToken)
		}

		var payload struct {
			Data      []map[string]any `json:"data"`
			NextToken string           `json:"next_token"`
		}
		rawBody, err := c.doJSON(ctx, accessToken, path, pageParams, &payload, retry)
		if err != nil {
			return nil, err
		}

		out = append(out, CollectionPage{
			Query:     cloneValues(pageParams),
			Data:      payload.Data,
			NextToken: strings.TrimSpace(payload.NextToken),
			RawBody:   rawBody,
		})

		nextToken = strings.TrimSpace(payload.NextToken)
		if nextToken == "" {
			break
		}
	}

	return out, nil
}

func (c *Client) FetchDocument(ctx context.Context, accessToken, path string, retry RetryConfig) (map[string]any, error) {
	result, err := c.FetchDocumentResult(ctx, accessToken, path, retry)
	if err != nil {
		return nil, err
	}
	return result.Value, nil
}

func (c *Client) FetchDocumentResult(ctx context.Context, accessToken, path string, retry RetryConfig) (DocumentResult, error) {
	var out map[string]any
	rawBody, err := c.doJSON(ctx, accessToken, path, nil, &out, retry)
	if err != nil {
		return DocumentResult{}, err
	}
	return DocumentResult{
		Value:   out,
		RawBody: rawBody,
	}, nil
}

func (c *Client) doJSON(ctx context.Context, accessToken, path string, params url.Values, target any, retry RetryConfig) (json.RawMessage, error) {
	maxAttempts := retry.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	currentToken := accessToken
	refreshed := false

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		reqURL := APIBaseURL + path
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+currentToken)
		req.Header.Set("Accept", "application/json")

		if c.RateLimiter != nil {
			if err := c.RateLimiter.Wait(ctx); err != nil {
				return nil, err
			}
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			apiErr := &APIError{Path: path, Query: cloneValues(params), Attempt: attempt, Err: err}
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

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode >= 400 {
			apiErr := &APIError{
				Path:         path,
				Query:        cloneValues(params),
				Attempt:      attempt,
				StatusCode:   resp.StatusCode,
				ResponseBody: truncate(strings.TrimSpace(string(body)), 512),
				RetryAfter:   parseRetryAfter(resp.Header.Get("Retry-After")),
			}
			if resp.StatusCode == http.StatusUnauthorized && !refreshed && retry.OnUnauthorized != nil {
				newToken, refreshErr := retry.OnUnauthorized(ctx, currentToken)
				if refreshErr == nil && newToken != "" {
					currentToken = newToken
					refreshed = true
					attempt-- // don't count the 401 against the retry budget
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

		if err := json.Unmarshal(body, target); err != nil {
			return nil, err
		}
		return json.RawMessage(body), nil
	}

	return nil, fmt.Errorf("oura api %s exceeded retry budget", path)
}

func cloneValues(values url.Values) url.Values {
	if len(values) == 0 {
		return nil
	}
	out := url.Values{}
	maps.Copy(out, values)
	return out
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
