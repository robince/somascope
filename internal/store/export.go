package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type DailyRecord struct {
	Provider      string          `json:"provider"`
	RecordKind    string          `json:"record_kind"`
	LocalDate     string          `json:"local_date"`
	ZoneOffset    string          `json:"zone_offset,omitempty"`
	SourceDevice  string          `json:"source_device,omitempty"`
	ExternalID    string          `json:"external_id,omitempty"`
	Summary       json.RawMessage `json:"summary"`
	RawDocumentID *int64          `json:"raw_document_id,omitempty"`
}

type SleepSession struct {
	Provider          string          `json:"provider"`
	LocalDate         string          `json:"local_date"`
	ZoneOffset        string          `json:"zone_offset,omitempty"`
	ExternalID        string          `json:"external_id,omitempty"`
	StartTime         string          `json:"start_time"`
	EndTime           string          `json:"end_time"`
	DurationMinutes   *int            `json:"duration_minutes,omitempty"`
	TimeInBedMinutes  *int            `json:"time_in_bed_minutes,omitempty"`
	EfficiencyPercent *float64        `json:"efficiency_percent,omitempty"`
	IsNap             bool            `json:"is_nap,omitempty"`
	Stages            json.RawMessage `json:"stages,omitempty"`
	Metrics           json.RawMessage `json:"metrics,omitempty"`
	RawDocumentID     *int64          `json:"raw_document_id,omitempty"`
}

type CanonicalExportRow struct {
	RecordType        string          `json:"record_type"`
	Provider          string          `json:"provider"`
	RecordKind        string          `json:"record_kind,omitempty"`
	LocalDate         string          `json:"local_date"`
	ZoneOffset        string          `json:"zone_offset,omitempty"`
	SourceDevice      string          `json:"source_device,omitempty"`
	ExternalID        string          `json:"external_id,omitempty"`
	StartTime         string          `json:"start_time,omitempty"`
	EndTime           string          `json:"end_time,omitempty"`
	DurationMinutes   *int            `json:"duration_minutes,omitempty"`
	TimeInBedMinutes  *int            `json:"time_in_bed_minutes,omitempty"`
	EfficiencyPercent *float64        `json:"efficiency_percent,omitempty"`
	IsNap             bool            `json:"is_nap,omitempty"`
	Summary           json.RawMessage `json:"summary,omitempty"`
	Stages            json.RawMessage `json:"stages,omitempty"`
	Metrics           json.RawMessage `json:"metrics,omitempty"`
	RawDocumentID     *int64          `json:"raw_document_id,omitempty"`
}

type RawExportRow struct {
	RecordType    string          `json:"record_type"`
	Provider      string          `json:"provider"`
	DocumentKind  string          `json:"document_kind"`
	ExternalID    string          `json:"external_id,omitempty"`
	LocalDate     string          `json:"local_date,omitempty"`
	ZoneOffset    string          `json:"zone_offset,omitempty"`
	RequestPath   string          `json:"request_path,omitempty"`
	RequestQuery  string          `json:"request_query,omitempty"`
	RequestStart  string          `json:"request_start,omitempty"`
	RequestEnd    string          `json:"request_end,omitempty"`
	FetchedAt     string          `json:"fetched_at"`
	DocumentKey   string          `json:"document_key,omitempty"`
	RawDocumentID int64           `json:"raw_document_id"`
	Payload       json.RawMessage `json:"payload"`
}

type RawExportFilter struct {
	StartDate     string
	EndDate       string
	DocumentKinds []string
}

type RawExportOptions struct {
	Provider      string   `json:"provider"`
	StartDate     string   `json:"start_date,omitempty"`
	EndDate       string   `json:"end_date,omitempty"`
	DocumentKinds []string `json:"document_kinds,omitempty"`
}

const rawExportDateExpr = `
	CASE
		WHEN COALESCE(local_date, '') <> '' THEN local_date
		WHEN COALESCE(request_start, '') <> '' THEN substr(request_start, 1, 10)
		WHEN COALESCE(request_end, '') <> '' THEN substr(request_end, 1, 10)
		ELSE ''
	END
`

func (s *Store) UpsertDailyRecord(ctx context.Context, record DailyRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO daily_records (
			provider, record_kind, local_date, zone_offset, source_device, external_id, summary_json, raw_document_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, record_kind, local_date, external_id) DO UPDATE SET
			zone_offset = excluded.zone_offset,
			source_device = excluded.source_device,
			summary_json = excluded.summary_json,
			raw_document_id = excluded.raw_document_id,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
	`, record.Provider, record.RecordKind, record.LocalDate, record.ZoneOffset, record.SourceDevice, record.ExternalID, string(record.Summary), record.RawDocumentID)
	return err
}

func (s *Store) InsertSleepSession(ctx context.Context, session SleepSession) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sleep_sessions (
			provider, local_date, zone_offset, external_id, start_time, end_time,
			duration_minutes, time_in_bed_minutes, efficiency_percent, is_nap,
			stages_json, metrics_json, raw_document_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, external_id) DO UPDATE SET
			local_date = excluded.local_date,
			zone_offset = excluded.zone_offset,
			start_time = excluded.start_time,
			end_time = excluded.end_time,
			duration_minutes = excluded.duration_minutes,
			time_in_bed_minutes = excluded.time_in_bed_minutes,
			efficiency_percent = excluded.efficiency_percent,
			is_nap = excluded.is_nap,
			stages_json = excluded.stages_json,
			metrics_json = excluded.metrics_json,
			raw_document_id = excluded.raw_document_id,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
	`, session.Provider, session.LocalDate, session.ZoneOffset, session.ExternalID, session.StartTime, session.EndTime,
		session.DurationMinutes, session.TimeInBedMinutes, session.EfficiencyPercent, boolToInt(session.IsNap),
		string(session.Stages), string(session.Metrics), session.RawDocumentID)
	return err
}

func (s *Store) CanonicalExportRows(ctx context.Context) ([]CanonicalExportRow, error) {
	const query = `
		SELECT
			'daily_record' AS record_type,
			provider,
			record_kind,
			local_date,
			zone_offset,
			source_device,
			external_id,
			'' AS start_time,
			'' AS end_time,
			NULL AS duration_minutes,
			NULL AS time_in_bed_minutes,
			NULL AS efficiency_percent,
			0 AS is_nap,
			summary_json,
			NULL AS stages_json,
			NULL AS metrics_json,
			raw_document_id
		FROM daily_records
		UNION ALL
		SELECT
			'sleep_session' AS record_type,
			provider,
			'' AS fact_kind,
			local_date,
			zone_offset,
			'' AS source_device,
			external_id,
			start_time,
			end_time,
			duration_minutes,
			time_in_bed_minutes,
			efficiency_percent,
			is_nap,
			NULL AS summary_json,
			stages_json,
			metrics_json,
			raw_document_id
		FROM sleep_sessions
		ORDER BY local_date DESC, provider ASC, record_type ASC;
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query canonical export rows: %w", err)
	}
	defer rows.Close()

	var out []CanonicalExportRow
	for rows.Next() {
		var row CanonicalExportRow
		var duration sql.NullInt64
		var timeInBed sql.NullInt64
		var efficiency sql.NullFloat64
		var isNap int
		var summary sql.NullString
		var stages sql.NullString
		var metrics sql.NullString
		var rawDocumentID sql.NullInt64

		if err := rows.Scan(
			&row.RecordType,
			&row.Provider,
			&row.RecordKind,
			&row.LocalDate,
			&row.ZoneOffset,
			&row.SourceDevice,
			&row.ExternalID,
			&row.StartTime,
			&row.EndTime,
			&duration,
			&timeInBed,
			&efficiency,
			&isNap,
			&summary,
			&stages,
			&metrics,
			&rawDocumentID,
		); err != nil {
			return nil, fmt.Errorf("scan canonical export row: %w", err)
		}

		if duration.Valid {
			value := int(duration.Int64)
			row.DurationMinutes = &value
		}
		if timeInBed.Valid {
			value := int(timeInBed.Int64)
			row.TimeInBedMinutes = &value
		}
		if efficiency.Valid {
			value := efficiency.Float64
			row.EfficiencyPercent = &value
		}
		row.IsNap = isNap != 0
		if summary.Valid {
			row.Summary = json.RawMessage(summary.String)
		}
		if stages.Valid {
			row.Stages = json.RawMessage(stages.String)
		}
		if metrics.Valid {
			row.Metrics = json.RawMessage(metrics.String)
		}
		if rawDocumentID.Valid {
			value := rawDocumentID.Int64
			row.RawDocumentID = &value
		}

		out = append(out, row)
	}

	return out, rows.Err()
}

func (s *Store) RawExportOptions(ctx context.Context, provider string) (RawExportOptions, error) {
	options := RawExportOptions{Provider: provider}

	if err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
		SELECT
			COALESCE(MIN(NULLIF(%s, '')), ''),
			COALESCE(MAX(NULLIF(%s, '')), '')
		FROM raw_documents
		WHERE provider = ?
	`, rawExportDateExpr, rawExportDateExpr), provider).Scan(&options.StartDate, &options.EndDate); err != nil {
		return RawExportOptions{}, fmt.Errorf("query raw export date range: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT document_kind
		FROM raw_documents
		WHERE provider = ? AND document_kind <> ''
		ORDER BY document_kind;
	`, provider)
	if err != nil {
		return RawExportOptions{}, fmt.Errorf("query raw export document kinds: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return RawExportOptions{}, fmt.Errorf("scan raw export document kind: %w", err)
		}
		options.DocumentKinds = append(options.DocumentKinds, kind)
	}

	return options, rows.Err()
}

func (s *Store) RawExportRows(ctx context.Context, provider string, filter RawExportFilter) ([]RawExportRow, error) {
	conditions := []string{"provider = ?"}
	args := []any{provider}

	startDate := strings.TrimSpace(filter.StartDate)
	if startDate != "" {
		conditions = append(conditions, fmt.Sprintf("(%s = '' OR %s >= ?)", rawExportDateExpr, rawExportDateExpr))
		args = append(args, startDate)
	}

	endDate := strings.TrimSpace(filter.EndDate)
	if endDate != "" {
		conditions = append(conditions, fmt.Sprintf("(%s = '' OR %s <= ?)", rawExportDateExpr, rawExportDateExpr))
		args = append(args, endDate)
	}

	documentKinds := compactStrings(filter.DocumentKinds)
	if len(documentKinds) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(documentKinds)), ",")
		conditions = append(conditions, "document_kind IN ("+placeholders+")")
		for _, kind := range documentKinds {
			args = append(args, kind)
		}
	}

	query := fmt.Sprintf(`
		SELECT
			'raw_document' AS record_type,
			provider,
			document_kind,
			COALESCE(external_id, ''),
			COALESCE(local_date, ''),
			COALESCE(zone_offset, ''),
			request_path,
			request_query,
			request_start,
			request_end,
			fetched_at,
			document_key,
			id,
			payload_json
		FROM raw_documents
		WHERE %s
		ORDER BY %s DESC, fetched_at DESC, id DESC;
	`, strings.Join(conditions, "\n\t\t\tAND "), rawExportDateExpr)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query raw export rows: %w", err)
	}
	defer rows.Close()

	var out []RawExportRow
	for rows.Next() {
		var row RawExportRow
		var payload string
		if err := rows.Scan(
			&row.RecordType,
			&row.Provider,
			&row.DocumentKind,
			&row.ExternalID,
			&row.LocalDate,
			&row.ZoneOffset,
			&row.RequestPath,
			&row.RequestQuery,
			&row.RequestStart,
			&row.RequestEnd,
			&row.FetchedAt,
			&row.DocumentKey,
			&row.RawDocumentID,
			&payload,
		); err != nil {
			return nil, fmt.Errorf("scan raw export row: %w", err)
		}
		row.Payload = json.RawMessage(payload)
		out = append(out, row)
	}

	return out, rows.Err()
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
