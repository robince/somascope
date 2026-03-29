package oura

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
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
	HTTPClient *http.Client
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{HTTPClient: httpClient}
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

func (c *Client) FetchCollection(ctx context.Context, accessToken, path, startDate, endDate string) ([]map[string]any, error) {
	params := url.Values{}
	params.Set("start_date", startDate)
	params.Set("end_date", endDate)

	var out []map[string]any
	var nextToken string
	for {
		pageParams := url.Values{}
		maps.Copy(pageParams, params)
		if nextToken != "" {
			pageParams.Set("next_token", nextToken)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, APIBaseURL+path+"?"+pageParams.Encode(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/json")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, err
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("oura api %s failed: %s", path, strings.TrimSpace(string(body)))
		}

		var payload struct {
			Data      []map[string]any `json:"data"`
			NextToken string           `json:"next_token"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		out = append(out, payload.Data...)
		nextToken = strings.TrimSpace(payload.NextToken)
		if nextToken == "" {
			break
		}
	}

	return out, nil
}
