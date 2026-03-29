ALTER TABLE raw_documents ADD COLUMN document_key TEXT NOT NULL DEFAULT '';

UPDATE raw_documents
SET document_key = 'legacy:' || id
WHERE document_key = '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_raw_documents_provider_kind_key
    ON raw_documents(provider, document_kind, document_key);

CREATE TABLE IF NOT EXISTS sync_runs (
    id                       TEXT PRIMARY KEY,
    provider                 TEXT NOT NULL,
    status                   TEXT NOT NULL,
    mode                     TEXT NOT NULL,
    requested_start_date     TEXT,
    requested_end_date       TEXT,
    effective_start_date     TEXT,
    effective_end_date       TEXT,
    started_at               TEXT NOT NULL,
    updated_at               TEXT NOT NULL,
    finished_at              TEXT,
    current_entity_kind      TEXT,
    current_chunk_start_date TEXT,
    current_chunk_end_date   TEXT,
    rows_written             INTEGER NOT NULL DEFAULT 0,
    completed_chunks         INTEGER NOT NULL DEFAULT 0,
    total_chunks             INTEGER NOT NULL DEFAULT 0,
    retry_count              INTEGER NOT NULL DEFAULT 0,
    last_error_json          TEXT,
    created_at               TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_sync_runs_provider_started
    ON sync_runs(provider, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_sync_runs_provider_status
    ON sync_runs(provider, status, started_at DESC);

CREATE TABLE IF NOT EXISTS sync_run_entities (
    run_id                    TEXT NOT NULL REFERENCES sync_runs(id) ON DELETE CASCADE,
    entity_kind               TEXT NOT NULL,
    status                    TEXT NOT NULL,
    start_date                TEXT,
    end_date                  TEXT,
    cursor_value              TEXT NOT NULL DEFAULT '',
    rows_written              INTEGER NOT NULL DEFAULT 0,
    completed_chunks          INTEGER NOT NULL DEFAULT 0,
    total_chunks              INTEGER NOT NULL DEFAULT 0,
    current_chunk_start_date  TEXT,
    current_chunk_end_date    TEXT,
    last_chunk_completed_at   TEXT,
    last_error_json           TEXT,
    updated_at                TEXT NOT NULL,
    created_at                TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (run_id, entity_kind)
);

CREATE INDEX IF NOT EXISTS idx_sync_run_entities_run
    ON sync_run_entities(run_id, entity_kind);
