package oura

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/robince/somascope/internal/store"
)

func TestTokenRefreshRequiresReauthOnlyForTerminalOAuthErrors(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{err: &OAuthTokenError{StatusCode: http.StatusBadRequest, Code: "invalid_grant"}, want: true},
		{err: &OAuthTokenError{StatusCode: http.StatusBadRequest, Code: "invalid_token"}, want: true},
		{err: &OAuthTokenError{StatusCode: http.StatusTooManyRequests, Code: "temporarily_unavailable"}},
		{err: errors.New("connection reset")},
	}
	for _, tt := range tests {
		if got := tokenRefreshRequiresReauth(tt.err); got != tt.want {
			t.Fatalf("tokenRefreshRequiresReauth(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestAdvanceSyncStateDoesNotRewindCursor(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	ctx := context.Background()
	if err := st.UpsertSyncState(ctx, "oura", "sleep", "2026-08-20", "2026-08-20T12:00:00Z"); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}
	if err := advanceSyncState(ctx, st, "sleep", "2026-07-01", "2026-08-22T12:00:00Z"); err != nil {
		t.Fatalf("advance cursor: %v", err)
	}
	cursor, _, err := st.SyncState(ctx, "oura", "sleep")
	if err != nil {
		t.Fatalf("load cursor: %v", err)
	}
	if cursor != "2026-08-20" {
		t.Fatalf("expected cursor to remain 2026-08-20, got %s", cursor)
	}
}
