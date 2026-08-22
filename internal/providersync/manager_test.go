package providersync

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/robince/somascope/internal/store"
)

func TestWithProviderIdleRejectsConnectionChangeDuringSync(t *testing.T) {
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "somascope.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	manager, err := NewManager(st)
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	t.Cleanup(manager.Shutdown)
	now := time.Now().UTC().Format(time.RFC3339)
	if err := st.CreateSyncRun(context.Background(), store.SyncRun{
		ID:        "active-run",
		Provider:  "google_health",
		Status:    "running",
		Mode:      "incremental",
		StartedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed active sync: %v", err)
	}
	called := false
	err = manager.WithProviderIdle("google_health", func() error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrProviderSyncActive) {
		t.Fatalf("expected ErrProviderSyncActive, got %v", err)
	}
	if called {
		t.Fatal("connection change ran during an active sync")
	}
}
