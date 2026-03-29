package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("not found")

type Connection struct {
	Provider          string
	ExternalAccountID string
	AccessToken       string
	RefreshToken      string
	TokenExpiresAt    string
	Scope             string
	Status            string
	ConnectedAt       string
	DisconnectedAt    string
}

type RawDocument struct {
	Provider     string
	DocumentKind string
	ExternalID   string
	LocalDate    string
	ZoneOffset   string
	RequestPath  string
	RequestQuery string
	RequestStart string
	RequestEnd   string
	Payload      json.RawMessage
	FetchedAt    string
	DocumentKey  string
}

type ProviderOverview struct {
	Provider          string `json:"provider"`
	Configured        bool   `json:"configured"`
	Connected         bool   `json:"connected"`
	Status            string `json:"status"`
	Scope             string `json:"scope,omitempty"`
	ConnectedAt       string `json:"connected_at,omitempty"`
	TokenExpiresAt    string `json:"token_expires_at,omitempty"`
	LastSyncAt        string `json:"last_sync_at,omitempty"`
	DailyRecordCount  int    `json:"daily_record_count"`
	SleepSessionCount int    `json:"sleep_session_count"`
}

type ProviderCredential struct {
	Provider      string
	ClientID      string
	ClientSecret  string
	RedirectURI   string
	DefaultScopes string
	Notes         string
}

type SyncStateEntry struct {
	Provider    string `json:"provider"`
	EntityKind  string `json:"entity_kind"`
	CursorValue string `json:"cursor_value"`
	SyncedAt    string `json:"synced_at,omitempty"`
}

func (s *Store) SetAppSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO app_settings(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
	`, key, value)
	return err
}

func (s *Store) AppSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM app_settings WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return value, err
}

func (s *Store) UpsertProviderCredential(ctx context.Context, credential ProviderCredential) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_credentials (
			provider, client_id, client_secret, redirect_uri, default_scopes, notes
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider) DO UPDATE SET
			client_id = excluded.client_id,
			client_secret = excluded.client_secret,
			redirect_uri = excluded.redirect_uri,
			default_scopes = excluded.default_scopes,
			notes = excluded.notes,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
	`, credential.Provider, credential.ClientID, credential.ClientSecret, credential.RedirectURI, credential.DefaultScopes, credential.Notes)
	return err
}

func (s *Store) ProviderCredentialByProvider(ctx context.Context, provider string) (ProviderCredential, error) {
	var credential ProviderCredential
	err := s.db.QueryRowContext(ctx, `
		SELECT provider, client_id, client_secret, redirect_uri, default_scopes, notes
		FROM provider_credentials
		WHERE provider = ?
	`, provider).Scan(
		&credential.Provider,
		&credential.ClientID,
		&credential.ClientSecret,
		&credential.RedirectURI,
		&credential.DefaultScopes,
		&credential.Notes,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderCredential{}, ErrNotFound
	}
	return credential, err
}

func (s *Store) ProviderCredentials(ctx context.Context) ([]ProviderCredential, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, client_id, client_secret, redirect_uri, default_scopes, notes
		FROM provider_credentials
		ORDER BY provider ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProviderCredential
	for rows.Next() {
		var credential ProviderCredential
		if err := rows.Scan(
			&credential.Provider,
			&credential.ClientID,
			&credential.ClientSecret,
			&credential.RedirectURI,
			&credential.DefaultScopes,
			&credential.Notes,
		); err != nil {
			return nil, err
		}
		out = append(out, credential)
	}
	return out, rows.Err()
}

func (s *Store) UpsertConnection(ctx context.Context, connection Connection) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO connections (
			provider, external_account_id, access_token, refresh_token, token_expires_at,
			scope, status, connected_at, disconnected_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider) DO UPDATE SET
			external_account_id = excluded.external_account_id,
			access_token = excluded.access_token,
			refresh_token = excluded.refresh_token,
			token_expires_at = excluded.token_expires_at,
			scope = excluded.scope,
			status = excluded.status,
			connected_at = excluded.connected_at,
			disconnected_at = excluded.disconnected_at,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
	`, connection.Provider, connection.ExternalAccountID, connection.AccessToken, connection.RefreshToken,
		nullIfEmpty(connection.TokenExpiresAt), connection.Scope, connection.Status,
		nullIfEmpty(connection.ConnectedAt), nullIfEmpty(connection.DisconnectedAt))
	return err
}

func (s *Store) ConnectionByProvider(ctx context.Context, provider string) (Connection, error) {
	var connection Connection
	var externalAccountID sql.NullString
	var refreshToken sql.NullString
	var tokenExpiresAt sql.NullString
	var scope sql.NullString
	var connectedAt sql.NullString
	var disconnectedAt sql.NullString

	err := s.db.QueryRowContext(ctx, `
			SELECT provider, external_account_id, access_token, refresh_token, token_expires_at,
				scope, status, connected_at, disconnected_at
			FROM connections
			WHERE provider = ?
		`, provider).Scan(
		&connection.Provider,
		&externalAccountID,
		&connection.AccessToken,
		&refreshToken,
		&tokenExpiresAt,
		&scope,
		&connection.Status,
		&connectedAt,
		&disconnectedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Connection{}, ErrNotFound
	}
	if err != nil {
		return Connection{}, err
	}
	connection.ExternalAccountID = externalAccountID.String
	connection.RefreshToken = refreshToken.String
	connection.TokenExpiresAt = tokenExpiresAt.String
	connection.Scope = scope.String
	connection.ConnectedAt = connectedAt.String
	connection.DisconnectedAt = disconnectedAt.String
	return connection, nil
}

func (s *Store) UpsertSyncState(ctx context.Context, provider, entityKind, cursorValue, syncedAt string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_state(provider, entity_kind, cursor_value, synced_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(provider, entity_kind) DO UPDATE SET
			cursor_value = excluded.cursor_value,
			synced_at = excluded.synced_at,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
	`, provider, entityKind, cursorValue, nullIfEmpty(syncedAt))
	return err
}

func (s *Store) SyncState(ctx context.Context, provider, entityKind string) (string, string, error) {
	var cursorValue string
	var syncedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT cursor_value, synced_at
		FROM sync_state
		WHERE provider = ? AND entity_kind = ?
	`, provider, entityKind).Scan(&cursorValue, &syncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return cursorValue, syncedAt.String, nil
}

func (s *Store) InsertRawDocument(ctx context.Context, doc RawDocument) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO raw_documents (
			provider, document_kind, external_id, local_date, zone_offset,
			request_path, request_query, request_start, request_end,
			payload_json, fetched_at, document_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, doc.Provider, doc.DocumentKind, nullIfEmpty(doc.ExternalID), nullIfEmpty(doc.LocalDate), nullIfEmpty(doc.ZoneOffset),
		doc.RequestPath, doc.RequestQuery, doc.RequestStart, doc.RequestEnd,
		string(doc.Payload), doc.FetchedAt, doc.DocumentKey)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *Store) UpsertRawDocument(ctx context.Context, doc RawDocument) (int64, error) {
	if strings.TrimSpace(doc.DocumentKey) == "" {
		return s.InsertRawDocument(ctx, doc)
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO raw_documents (
			provider, document_kind, external_id, local_date, zone_offset,
			request_path, request_query, request_start, request_end,
			payload_json, fetched_at, document_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, document_kind, document_key) DO UPDATE SET
			external_id = excluded.external_id,
			local_date = excluded.local_date,
			zone_offset = excluded.zone_offset,
			request_path = excluded.request_path,
			request_query = excluded.request_query,
			request_start = excluded.request_start,
			request_end = excluded.request_end,
			payload_json = excluded.payload_json,
			fetched_at = excluded.fetched_at
		RETURNING id
	`, doc.Provider, doc.DocumentKind, nullIfEmpty(doc.ExternalID), nullIfEmpty(doc.LocalDate), nullIfEmpty(doc.ZoneOffset),
		doc.RequestPath, doc.RequestQuery, doc.RequestStart, doc.RequestEnd,
		string(doc.Payload), doc.FetchedAt, doc.DocumentKey).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) SyncStatesByProvider(ctx context.Context, provider string) ([]SyncStateEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, entity_kind, cursor_value, synced_at
		FROM sync_state
		WHERE provider = ?
		ORDER BY entity_kind ASC
	`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SyncStateEntry
	for rows.Next() {
		var entry SyncStateEntry
		var syncedAt sql.NullString
		if err := rows.Scan(&entry.Provider, &entry.EntityKind, &entry.CursorValue, &syncedAt); err != nil {
			return nil, err
		}
		entry.SyncedAt = syncedAt.String
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (s *Store) ProviderOverview(ctx context.Context, provider string, configured bool) (ProviderOverview, error) {
	out := ProviderOverview{
		Provider:   provider,
		Configured: configured,
		Status:     "disconnected",
	}

	if connection, err := s.ConnectionByProvider(ctx, provider); err == nil {
		out.Status = connection.Status
		out.Connected = connection.Status == "connected" && connection.AccessToken != ""
		out.Scope = connection.Scope
		out.ConnectedAt = connection.ConnectedAt
		out.TokenExpiresAt = connection.TokenExpiresAt
	} else if !errors.Is(err, ErrNotFound) {
		return ProviderOverview{}, err
	}

	if syncedAt, err := s.LatestProviderSyncAt(ctx, provider); err == nil {
		out.LastSyncAt = syncedAt
	} else if !errors.Is(err, ErrNotFound) {
		return ProviderOverview{}, err
	}

	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM daily_records WHERE provider = ?", provider).Scan(&out.DailyRecordCount); err != nil {
		return ProviderOverview{}, err
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sleep_sessions WHERE provider = ?", provider).Scan(&out.SleepSessionCount); err != nil {
		return ProviderOverview{}, err
	}
	return out, nil
}

func (s *Store) LatestProviderSyncAt(ctx context.Context, provider string) (string, error) {
	var syncedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT MAX(synced_at)
		FROM sync_state
		WHERE provider = ?
	`, provider).Scan(&syncedAt)
	if err != nil {
		return "", err
	}
	if !syncedAt.Valid || syncedAt.String == "" {
		return "", ErrNotFound
	}
	return syncedAt.String, nil
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func isoNow() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func isoIfValid(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func wrapErr(message string, err error) error {
	return fmt.Errorf("%s: %w", message, err)
}
