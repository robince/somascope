package oura

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClientReusesRefreshedTokenAcrossRequests(t *testing.T) {
	currentToken := "old-token"
	oldTokenRequests := 0
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") == "Bearer old-token" {
			oldTokenRequests++
			return testJSONResponse(http.StatusUnauthorized, `{"error":"expired"}`), nil
		}
		return testJSONResponse(http.StatusOK, `{"id":"user-1"}`), nil
	})})
	retry := RetryConfig{
		MaxAttempts: 2,
		CurrentAccessToken: func() string {
			return currentToken
		},
		OnUnauthorized: func(_ context.Context, _ string) (string, error) {
			currentToken = "new-token"
			return currentToken, nil
		},
	}
	if _, err := client.FetchDocument(context.Background(), "old-token", "/first", retry); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, err := client.FetchDocument(context.Background(), "old-token", "/second", retry); err != nil {
		t.Fatalf("second request: %v", err)
	}
	if oldTokenRequests != 1 {
		t.Fatalf("expected stale token only on the initial request, got %d uses", oldTokenRequests)
	}
}

func TestFetchCollectionPagesRejectsRepeatedToken(t *testing.T) {
	client := NewClient(&http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return testJSONResponse(http.StatusOK, `{"data":[],"next_token":"same-token"}`), nil
	})})
	_, err := client.FetchCollectionPages(context.Background(), "token", "/collection", nil, RetryConfig{MaxAttempts: 1})
	if err == nil || !strings.Contains(err.Error(), "repeated next token") {
		t.Fatalf("expected repeated-token error, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func testJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
