package providersync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robince/somascope/internal/store"
)

type Task func(context.Context, *Tracker) error

type Manager struct {
	store     *store.Store
	mu        sync.Mutex
	active    map[string]string
	ctx       context.Context
	cancelAll context.CancelFunc
}

type Tracker struct {
	store    *store.Store
	run      store.SyncRun
	entities map[string]store.SyncRunEntity
	mu       sync.Mutex
}

func NewManager(st *store.Store) (*Manager, error) {
	ctx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		store:     st,
		active:    map[string]string{},
		ctx:       ctx,
		cancelAll: cancel,
	}
	if err := st.MarkRunningSyncRunsInterrupted(context.Background(), "sync interrupted because somascope restarted"); err != nil {
		cancel()
		return nil, err
	}
	return manager, nil
}

func (m *Manager) Shutdown() {
	m.cancelAll()
}

func (m *Manager) Start(provider, mode, requestedStartDate, requestedEndDate string, task Task) (store.SyncRun, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	currentRun, err := m.store.CurrentSyncRunByProvider(context.Background(), provider)
	switch {
	case err == nil:
		return currentRun, true, nil
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return store.SyncRun{}, false, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	run := store.SyncRun{
		ID:                 newRunID(),
		Provider:           provider,
		Status:             "running",
		Mode:               mode,
		RequestedStartDate: requestedStartDate,
		RequestedEndDate:   requestedEndDate,
		StartedAt:          now,
		UpdatedAt:          now,
	}
	if err := m.store.CreateSyncRun(context.Background(), run); err != nil {
		return store.SyncRun{}, false, err
	}

	m.active[provider] = run.ID
	tracker := &Tracker{
		store:    m.store,
		run:      run,
		entities: map[string]store.SyncRunEntity{},
	}

	go func() {
		defer m.finish(provider, run.ID)
		if err := task(m.ctx, tracker); err != nil {
			if failErr := tracker.FailUnknown(err); failErr != nil {
				log.Printf("warning: failed updating sync run %s failure state: %v", run.ID, failErr)
			}
			return
		}
		if err := tracker.Succeed(); err != nil {
			log.Printf("warning: failed marking sync run %s succeeded: %v", run.ID, err)
		}
	}()

	return run, false, nil
}

func (m *Manager) finish(provider, runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if activeRunID, ok := m.active[provider]; ok && activeRunID == runID {
		delete(m.active, provider)
	}
}

func (t *Tracker) Run() store.SyncRun {
	t.mu.Lock()
	defer t.mu.Unlock()
	run := t.run
	run.Entities = orderedEntities(t.entities)
	return run
}

func (t *Tracker) SetEffectiveRange(startDate, endDate string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.run.EffectiveStartDate = startDate
	t.run.EffectiveEndDate = endDate
	t.run.UpdatedAt = now()
	return t.store.UpdateSyncRun(context.Background(), t.run)
}

func (t *Tracker) StartEntity(entityKind, startDate, endDate string, totalChunks int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	entity := t.entity(entityKind)
	entity.Status = "running"
	entity.StartDate = startDate
	entity.EndDate = endDate
	entity.TotalChunks = totalChunks
	entity.UpdatedAt = now()
	t.entities[entityKind] = entity
	t.run.CurrentEntityKind = entityKind
	t.run.TotalChunks += totalChunks
	t.run.UpdatedAt = entity.UpdatedAt
	t.log("info", "entity_started", map[string]any{
		"entity_kind":  entityKind,
		"start_date":   startDate,
		"end_date":     endDate,
		"total_chunks": totalChunks,
	})
	if err := t.store.UpsertSyncRunEntity(context.Background(), entity); err != nil {
		return err
	}
	return t.store.UpdateSyncRun(context.Background(), t.run)
}

func (t *Tracker) StartChunk(entityKind, chunkStartDate, chunkEndDate string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	entity := t.entity(entityKind)
	entity.Status = "running"
	entity.CurrentChunkStartDate = chunkStartDate
	entity.CurrentChunkEndDate = chunkEndDate
	entity.UpdatedAt = now()
	t.entities[entityKind] = entity
	t.run.CurrentEntityKind = entityKind
	t.run.CurrentChunkStartDate = chunkStartDate
	t.run.CurrentChunkEndDate = chunkEndDate
	t.run.UpdatedAt = entity.UpdatedAt
	t.log("info", "chunk_started", map[string]any{
		"entity_kind":      entityKind,
		"chunk_start_date": chunkStartDate,
		"chunk_end_date":   chunkEndDate,
	})
	if err := t.store.UpsertSyncRunEntity(context.Background(), entity); err != nil {
		return err
	}
	return t.store.UpdateSyncRun(context.Background(), t.run)
}

func (t *Tracker) CompleteChunk(entityKind, cursor string, rowsWritten int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	entity := t.entity(entityKind)
	entity.CursorValue = cursor
	entity.RowsWritten += rowsWritten
	entity.CompletedChunks++
	entity.CurrentChunkStartDate = ""
	entity.CurrentChunkEndDate = ""
	entity.LastChunkCompletedAt = now()
	entity.UpdatedAt = entity.LastChunkCompletedAt
	entity.LastError = nil
	t.entities[entityKind] = entity
	t.run.RowsWritten += rowsWritten
	t.run.CompletedChunks++
	t.run.CurrentChunkStartDate = ""
	t.run.CurrentChunkEndDate = ""
	t.run.UpdatedAt = entity.UpdatedAt
	t.run.LastError = nil
	t.log("info", "chunk_completed", map[string]any{
		"entity_kind": entityKind,
		"rows":        rowsWritten,
		"cursor":      cursor,
	})
	if err := t.store.UpsertSyncRunEntity(context.Background(), entity); err != nil {
		return err
	}
	return t.store.UpdateSyncRun(context.Background(), t.run)
}

func (t *Tracker) CompleteEntity(entityKind string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	entity := t.entity(entityKind)
	entity.Status = "succeeded"
	entity.CurrentChunkStartDate = ""
	entity.CurrentChunkEndDate = ""
	entity.UpdatedAt = now()
	t.entities[entityKind] = entity
	t.run.CurrentChunkStartDate = ""
	t.run.CurrentChunkEndDate = ""
	t.run.UpdatedAt = entity.UpdatedAt
	t.log("info", "entity_completed", map[string]any{
		"entity_kind": entityKind,
		"rows":        entity.RowsWritten,
	})
	if err := t.store.UpsertSyncRunEntity(context.Background(), entity); err != nil {
		return err
	}
	return t.store.UpdateSyncRun(context.Background(), t.run)
}

func (t *Tracker) Retry(entityKind string, syncErr *store.SyncError, backoff time.Duration) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	entity := t.entity(entityKind)
	entity.LastError = syncErr
	entity.UpdatedAt = now()
	t.entities[entityKind] = entity
	t.run.RetryCount++
	t.run.LastError = syncErr
	t.run.UpdatedAt = entity.UpdatedAt
	t.log("warn", "retry_scheduled", map[string]any{
		"entity_kind":      entityKind,
		"chunk_start_date": syncErr.ChunkStartDate,
		"chunk_end_date":   syncErr.ChunkEndDate,
		"http_status":      syncErr.HTTPStatus,
		"attempt":          syncErr.Attempt,
		"backoff_ms":       backoff.Milliseconds(),
		"message":          syncErr.Message,
	})
	if err := t.store.UpsertSyncRunEntity(context.Background(), entity); err != nil {
		return err
	}
	return t.store.UpdateSyncRun(context.Background(), t.run)
}

func (t *Tracker) Fail(entityKind string, syncErr *store.SyncError) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	entity := t.entity(entityKind)
	entity.Status = "failed"
	entity.LastError = syncErr
	entity.CurrentChunkStartDate = ""
	entity.CurrentChunkEndDate = ""
	entity.UpdatedAt = now()
	t.entities[entityKind] = entity
	t.run.Status = "failed"
	t.run.CurrentEntityKind = entityKind
	t.run.CurrentChunkStartDate = ""
	t.run.CurrentChunkEndDate = ""
	t.run.LastError = syncErr
	t.run.UpdatedAt = entity.UpdatedAt
	t.run.FinishedAt = entity.UpdatedAt
	t.log("error", "run_failed", map[string]any{
		"entity_kind":      entityKind,
		"chunk_start_date": syncErr.ChunkStartDate,
		"chunk_end_date":   syncErr.ChunkEndDate,
		"http_status":      syncErr.HTTPStatus,
		"attempt":          syncErr.Attempt,
		"message":          syncErr.Message,
		"response_body":    syncErr.ResponseBody,
	})
	if err := t.store.UpsertSyncRunEntity(context.Background(), entity); err != nil {
		return err
	}
	return t.store.UpdateSyncRun(context.Background(), t.run)
}

func (t *Tracker) FailUnknown(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.run.Status != "running" {
		return nil
	}
	now := now()
	t.run.Status = "failed"
	t.run.UpdatedAt = now
	t.run.FinishedAt = now
	t.run.LastError = &store.SyncError{
		At:      now,
		Message: err.Error(),
	}
	t.log("error", "run_failed", map[string]any{
		"message": err.Error(),
	})
	return t.store.UpdateSyncRun(context.Background(), t.run)
}

func (t *Tracker) Succeed() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.run.Status != "running" {
		return nil
	}
	now := now()
	t.run.Status = "succeeded"
	t.run.CurrentEntityKind = ""
	t.run.CurrentChunkStartDate = ""
	t.run.CurrentChunkEndDate = ""
	t.run.LastError = nil
	t.run.UpdatedAt = now
	t.run.FinishedAt = now
	t.log("info", "run_completed", map[string]any{
		"rows_written":     t.run.RowsWritten,
		"completed_chunks": t.run.CompletedChunks,
		"retry_count":      t.run.RetryCount,
	})
	return t.store.UpdateSyncRun(context.Background(), t.run)
}

func (t *Tracker) entity(entityKind string) store.SyncRunEntity {
	if entity, ok := t.entities[entityKind]; ok {
		return entity
	}
	return store.SyncRunEntity{
		RunID:      t.run.ID,
		EntityKind: entityKind,
		Status:     "pending",
		UpdatedAt:  now(),
	}
}

func (t *Tracker) log(level, event string, fields map[string]any) {
	message := fmt.Sprintf("sync level=%s event=%s provider=%s run_id=%s", level, event, t.run.Provider, t.run.ID)
	for key, value := range fields {
		if value == nil || value == "" {
			continue
		}
		message += fmt.Sprintf(" %s=%v", key, value)
	}
	log.Print(message)
}

func newRunID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("sync_%d", time.Now().UTC().UnixNano())
	}
	return "sync_" + hex.EncodeToString(buffer)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func orderedEntities(entities map[string]store.SyncRunEntity) []store.SyncRunEntity {
	if len(entities) == 0 {
		return nil
	}
	out := make([]store.SyncRunEntity, 0, len(entities))
	for _, entity := range entities {
		out = append(out, entity)
	}
	return out
}
