package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type SyncError struct {
	At             string `json:"at,omitempty"`
	EntityKind     string `json:"entity_kind,omitempty"`
	ChunkStartDate string `json:"chunk_start_date,omitempty"`
	ChunkEndDate   string `json:"chunk_end_date,omitempty"`
	Operation      string `json:"operation,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	HTTPStatus     int    `json:"http_status,omitempty"`
	Attempt        int    `json:"attempt,omitempty"`
	Retriable      bool   `json:"retriable,omitempty"`
	Message        string `json:"message,omitempty"`
	ResponseBody   string `json:"response_body,omitempty"`
}

type SyncRunEntity struct {
	RunID                 string     `json:"run_id,omitempty"`
	EntityKind            string     `json:"entity_kind"`
	Status                string     `json:"status"`
	StartDate             string     `json:"start_date,omitempty"`
	EndDate               string     `json:"end_date,omitempty"`
	CursorValue           string     `json:"cursor_value,omitempty"`
	RowsWritten           int        `json:"rows_written"`
	CompletedChunks       int        `json:"completed_chunks"`
	TotalChunks           int        `json:"total_chunks"`
	CurrentChunkStartDate string     `json:"current_chunk_start_date,omitempty"`
	CurrentChunkEndDate   string     `json:"current_chunk_end_date,omitempty"`
	LastChunkCompletedAt  string     `json:"last_chunk_completed_at,omitempty"`
	LastError             *SyncError `json:"last_error,omitempty"`
	UpdatedAt             string     `json:"updated_at,omitempty"`
}

type SyncRun struct {
	ID                    string          `json:"id"`
	Provider              string          `json:"provider"`
	Status                string          `json:"status"`
	Mode                  string          `json:"mode"`
	RequestedStartDate    string          `json:"requested_start_date,omitempty"`
	RequestedEndDate      string          `json:"requested_end_date,omitempty"`
	EffectiveStartDate    string          `json:"effective_start_date,omitempty"`
	EffectiveEndDate      string          `json:"effective_end_date,omitempty"`
	StartedAt             string          `json:"started_at"`
	UpdatedAt             string          `json:"updated_at"`
	FinishedAt            string          `json:"finished_at,omitempty"`
	CurrentEntityKind     string          `json:"current_entity_kind,omitempty"`
	CurrentChunkStartDate string          `json:"current_chunk_start_date,omitempty"`
	CurrentChunkEndDate   string          `json:"current_chunk_end_date,omitempty"`
	RowsWritten           int             `json:"rows_written"`
	CompletedChunks       int             `json:"completed_chunks"`
	TotalChunks           int             `json:"total_chunks"`
	RetryCount            int             `json:"retry_count"`
	LastError             *SyncError      `json:"last_error,omitempty"`
	Entities              []SyncRunEntity `json:"entities,omitempty"`
}

func (s *Store) CreateSyncRun(ctx context.Context, run SyncRun) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_runs (
			id, provider, status, mode, requested_start_date, requested_end_date,
			effective_start_date, effective_end_date, started_at, updated_at, finished_at,
			current_entity_kind, current_chunk_start_date, current_chunk_end_date,
			rows_written, completed_chunks, total_chunks, retry_count, last_error_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, run.ID, run.Provider, run.Status, run.Mode,
		nullIfEmpty(run.RequestedStartDate), nullIfEmpty(run.RequestedEndDate),
		nullIfEmpty(run.EffectiveStartDate), nullIfEmpty(run.EffectiveEndDate),
		run.StartedAt, run.UpdatedAt, nullIfEmpty(run.FinishedAt),
		nullIfEmpty(run.CurrentEntityKind), nullIfEmpty(run.CurrentChunkStartDate), nullIfEmpty(run.CurrentChunkEndDate),
		run.RowsWritten, run.CompletedChunks, run.TotalChunks, run.RetryCount, nullIfEmpty(mustJSONText(run.LastError)))
	return err
}

func (s *Store) UpdateSyncRun(ctx context.Context, run SyncRun) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE sync_runs
		SET
			status = ?,
			mode = ?,
			requested_start_date = ?,
			requested_end_date = ?,
			effective_start_date = ?,
			effective_end_date = ?,
			updated_at = ?,
			finished_at = ?,
			current_entity_kind = ?,
			current_chunk_start_date = ?,
			current_chunk_end_date = ?,
			rows_written = ?,
			completed_chunks = ?,
			total_chunks = ?,
			retry_count = ?,
			last_error_json = ?
		WHERE id = ?
	`, run.Status, run.Mode,
		nullIfEmpty(run.RequestedStartDate), nullIfEmpty(run.RequestedEndDate),
		nullIfEmpty(run.EffectiveStartDate), nullIfEmpty(run.EffectiveEndDate),
		run.UpdatedAt, nullIfEmpty(run.FinishedAt),
		nullIfEmpty(run.CurrentEntityKind), nullIfEmpty(run.CurrentChunkStartDate), nullIfEmpty(run.CurrentChunkEndDate),
		run.RowsWritten, run.CompletedChunks, run.TotalChunks, run.RetryCount, nullIfEmpty(mustJSONText(run.LastError)),
		run.ID)
	return err
}

func (s *Store) UpsertSyncRunEntity(ctx context.Context, entity SyncRunEntity) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_run_entities (
			run_id, entity_kind, status, start_date, end_date, cursor_value, rows_written,
			completed_chunks, total_chunks, current_chunk_start_date, current_chunk_end_date,
			last_chunk_completed_at, last_error_json, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, entity_kind) DO UPDATE SET
			status = excluded.status,
			start_date = excluded.start_date,
			end_date = excluded.end_date,
			cursor_value = excluded.cursor_value,
			rows_written = excluded.rows_written,
			completed_chunks = excluded.completed_chunks,
			total_chunks = excluded.total_chunks,
			current_chunk_start_date = excluded.current_chunk_start_date,
			current_chunk_end_date = excluded.current_chunk_end_date,
			last_chunk_completed_at = excluded.last_chunk_completed_at,
			last_error_json = excluded.last_error_json,
			updated_at = excluded.updated_at
	`, entity.RunID, entity.EntityKind, entity.Status,
		nullIfEmpty(entity.StartDate), nullIfEmpty(entity.EndDate), entity.CursorValue,
		entity.RowsWritten, entity.CompletedChunks, entity.TotalChunks,
		nullIfEmpty(entity.CurrentChunkStartDate), nullIfEmpty(entity.CurrentChunkEndDate),
		nullIfEmpty(entity.LastChunkCompletedAt), nullIfEmpty(mustJSONText(entity.LastError)), entity.UpdatedAt)
	return err
}

func (s *Store) CurrentSyncRunByProvider(ctx context.Context, provider string) (SyncRun, error) {
	return s.syncRunByQuery(ctx, `
		SELECT id, provider, status, mode, requested_start_date, requested_end_date,
			effective_start_date, effective_end_date, started_at, updated_at, finished_at,
			current_entity_kind, current_chunk_start_date, current_chunk_end_date,
			rows_written, completed_chunks, total_chunks, retry_count, last_error_json
		FROM sync_runs
		WHERE provider = ? AND status = 'running'
		ORDER BY started_at DESC
		LIMIT 1
	`, provider)
}

func (s *Store) LatestFinishedSyncRunByProvider(ctx context.Context, provider string) (SyncRun, error) {
	return s.syncRunByQuery(ctx, `
		SELECT id, provider, status, mode, requested_start_date, requested_end_date,
			effective_start_date, effective_end_date, started_at, updated_at, finished_at,
			current_entity_kind, current_chunk_start_date, current_chunk_end_date,
			rows_written, completed_chunks, total_chunks, retry_count, last_error_json
		FROM sync_runs
		WHERE provider = ? AND status <> 'running'
		ORDER BY COALESCE(finished_at, updated_at) DESC
		LIMIT 1
	`, provider)
}

func (s *Store) LatestSuccessfulSyncRunByProvider(ctx context.Context, provider string) (SyncRun, error) {
	return s.syncRunByQuery(ctx, `
		SELECT id, provider, status, mode, requested_start_date, requested_end_date,
			effective_start_date, effective_end_date, started_at, updated_at, finished_at,
			current_entity_kind, current_chunk_start_date, current_chunk_end_date,
			rows_written, completed_chunks, total_chunks, retry_count, last_error_json
		FROM sync_runs
		WHERE provider = ? AND status = 'succeeded'
		ORDER BY COALESCE(finished_at, updated_at) DESC
		LIMIT 1
	`, provider)
}

func (s *Store) MarkRunningSyncRunsInterrupted(ctx context.Context, message string) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider
		FROM sync_runs
		WHERE status = 'running'
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	now := isoNow()
	for rows.Next() {
		var id string
		var provider string
		if err := rows.Scan(&id, &provider); err != nil {
			return err
		}
		run := SyncRun{
			ID:         id,
			Provider:   provider,
			Status:     "interrupted",
			UpdatedAt:  now,
			FinishedAt: now,
			LastError: &SyncError{
				At:      now,
				Message: message,
			},
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE sync_runs
			SET status = ?, updated_at = ?, finished_at = ?, last_error_json = ?
			WHERE id = ?
		`, run.Status, run.UpdatedAt, run.FinishedAt, mustJSONText(run.LastError), run.ID); err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE sync_run_entities
			SET
				status = CASE WHEN status = 'running' THEN 'failed' ELSE status END,
				last_error_json = CASE
					WHEN status = 'running' THEN ?
					ELSE last_error_json
				END,
				updated_at = ?
			WHERE run_id = ?
		`, mustJSONText(run.LastError), now, run.ID); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) syncRunByQuery(ctx context.Context, query string, args ...any) (SyncRun, error) {
	var run SyncRun
	var requestedStartDate sql.NullString
	var requestedEndDate sql.NullString
	var effectiveStartDate sql.NullString
	var effectiveEndDate sql.NullString
	var finishedAt sql.NullString
	var currentEntityKind sql.NullString
	var currentChunkStartDate sql.NullString
	var currentChunkEndDate sql.NullString
	var lastError sql.NullString

	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&run.ID,
		&run.Provider,
		&run.Status,
		&run.Mode,
		&requestedStartDate,
		&requestedEndDate,
		&effectiveStartDate,
		&effectiveEndDate,
		&run.StartedAt,
		&run.UpdatedAt,
		&finishedAt,
		&currentEntityKind,
		&currentChunkStartDate,
		&currentChunkEndDate,
		&run.RowsWritten,
		&run.CompletedChunks,
		&run.TotalChunks,
		&run.RetryCount,
		&lastError,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncRun{}, ErrNotFound
	}
	if err != nil {
		return SyncRun{}, err
	}
	run.RequestedStartDate = requestedStartDate.String
	run.RequestedEndDate = requestedEndDate.String
	run.EffectiveStartDate = effectiveStartDate.String
	run.EffectiveEndDate = effectiveEndDate.String
	run.FinishedAt = finishedAt.String
	run.CurrentEntityKind = currentEntityKind.String
	run.CurrentChunkStartDate = currentChunkStartDate.String
	run.CurrentChunkEndDate = currentChunkEndDate.String
	run.LastError = decodeSyncError(lastError)

	entities, err := s.SyncRunEntities(ctx, run.ID)
	if err != nil {
		return SyncRun{}, err
	}
	run.Entities = entities
	return run, nil
}

func (s *Store) SyncRunEntities(ctx context.Context, runID string) ([]SyncRunEntity, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT run_id, entity_kind, status, start_date, end_date, cursor_value, rows_written,
			completed_chunks, total_chunks, current_chunk_start_date, current_chunk_end_date,
			last_chunk_completed_at, last_error_json, updated_at
		FROM sync_run_entities
		WHERE run_id = ?
		ORDER BY entity_kind ASC
	`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SyncRunEntity
	for rows.Next() {
		var entity SyncRunEntity
		var startDate sql.NullString
		var endDate sql.NullString
		var currentChunkStartDate sql.NullString
		var currentChunkEndDate sql.NullString
		var lastChunkCompletedAt sql.NullString
		var lastError sql.NullString
		if err := rows.Scan(
			&entity.RunID,
			&entity.EntityKind,
			&entity.Status,
			&startDate,
			&endDate,
			&entity.CursorValue,
			&entity.RowsWritten,
			&entity.CompletedChunks,
			&entity.TotalChunks,
			&currentChunkStartDate,
			&currentChunkEndDate,
			&lastChunkCompletedAt,
			&lastError,
			&entity.UpdatedAt,
		); err != nil {
			return nil, err
		}
		entity.StartDate = startDate.String
		entity.EndDate = endDate.String
		entity.CurrentChunkStartDate = currentChunkStartDate.String
		entity.CurrentChunkEndDate = currentChunkEndDate.String
		entity.LastChunkCompletedAt = lastChunkCompletedAt.String
		entity.LastError = decodeSyncError(lastError)
		out = append(out, entity)
	}
	return out, rows.Err()
}

func decodeSyncError(value sql.NullString) *SyncError {
	if !value.Valid || value.String == "" {
		return nil
	}
	var out SyncError
	if err := json.Unmarshal([]byte(value.String), &out); err != nil {
		return &SyncError{Message: value.String}
	}
	return &out
}

func mustJSONText(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil || string(data) == "null" {
		return ""
	}
	return string(data)
}
