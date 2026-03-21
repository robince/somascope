-- Local-first somascope schema.
-- V1 starts with config.json-backed provider settings, but these tables
-- define the SQLite shape for the next ingestion/export slice.

CREATE TABLE IF NOT EXISTS app_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS provider_credentials (
    provider         TEXT PRIMARY KEY,
    client_id        TEXT NOT NULL DEFAULT '',
    client_secret    TEXT NOT NULL DEFAULT '',
    redirect_uri     TEXT NOT NULL DEFAULT '',
    default_scopes   TEXT NOT NULL DEFAULT '',
    notes            TEXT NOT NULL DEFAULT '',
    updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS connections (
    provider              TEXT PRIMARY KEY,
    external_account_id   TEXT,
    access_token          TEXT NOT NULL DEFAULT '',
    refresh_token         TEXT NOT NULL DEFAULT '',
    token_expires_at      TEXT,
    scope                 TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'disconnected',
    connected_at          TEXT,
    disconnected_at       TEXT,
    updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS sync_state (
    provider      TEXT NOT NULL,
    entity_kind   TEXT NOT NULL,
    cursor_value  TEXT NOT NULL DEFAULT '',
    synced_at     TEXT,
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (provider, entity_kind)
);

CREATE TABLE IF NOT EXISTS raw_documents (
    id               INTEGER PRIMARY KEY,
    provider         TEXT NOT NULL,
    document_kind    TEXT NOT NULL,
    external_id      TEXT,
    local_date       TEXT,
    zone_offset      TEXT,
    payload_json     TEXT NOT NULL,
    fetched_at       TEXT NOT NULL,
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_raw_documents_provider_kind
    ON raw_documents(provider, document_kind, local_date);

CREATE TABLE IF NOT EXISTS daily_facts (
    id                 INTEGER PRIMARY KEY,
    provider           TEXT NOT NULL,
    fact_kind          TEXT NOT NULL,
    local_date         TEXT NOT NULL,
    zone_offset        TEXT NOT NULL DEFAULT '',
    source_device      TEXT NOT NULL DEFAULT '',
    external_id        TEXT NOT NULL DEFAULT '',
    summary_json       TEXT NOT NULL,
    raw_document_id    INTEGER REFERENCES raw_documents(id) ON DELETE SET NULL,
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(provider, fact_kind, local_date, external_id)
);

CREATE INDEX IF NOT EXISTS idx_daily_facts_provider_date
    ON daily_facts(provider, local_date DESC, fact_kind);

CREATE TABLE IF NOT EXISTS sleep_sessions (
    id                    INTEGER PRIMARY KEY,
    provider              TEXT NOT NULL,
    local_date            TEXT NOT NULL,
    zone_offset           TEXT NOT NULL DEFAULT '',
    external_id           TEXT NOT NULL DEFAULT '',
    start_time            TEXT NOT NULL,
    end_time              TEXT NOT NULL,
    duration_minutes      INTEGER,
    time_in_bed_minutes   INTEGER,
    efficiency_percent    REAL,
    is_nap                INTEGER NOT NULL DEFAULT 0,
    stages_json           TEXT NOT NULL DEFAULT '{}',
    metrics_json          TEXT NOT NULL DEFAULT '{}',
    raw_document_id       INTEGER REFERENCES raw_documents(id) ON DELETE SET NULL,
    created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(provider, external_id)
);

CREATE INDEX IF NOT EXISTS idx_sleep_sessions_provider_date
    ON sleep_sessions(provider, local_date DESC, start_time DESC);
