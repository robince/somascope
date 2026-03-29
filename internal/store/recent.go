package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

func (s *Store) RecentDailyRecords(ctx context.Context, provider string, limit int) ([]DailyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, record_kind, local_date, zone_offset, source_device, external_id, summary_json, raw_document_id
		FROM daily_records
		WHERE provider = ?
		ORDER BY local_date DESC, record_kind ASC
		LIMIT ?
	`, provider, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent daily records: %w", err)
	}
	defer rows.Close()

	var out []DailyRecord
	for rows.Next() {
		var record DailyRecord
		var summary string
		var rawDocumentID sql.NullInt64
		if err := rows.Scan(
			&record.Provider,
			&record.RecordKind,
			&record.LocalDate,
			&record.ZoneOffset,
			&record.SourceDevice,
			&record.ExternalID,
			&summary,
			&rawDocumentID,
		); err != nil {
			return nil, fmt.Errorf("scan recent daily record: %w", err)
		}
		record.Summary = json.RawMessage(summary)
		if rawDocumentID.Valid {
			value := rawDocumentID.Int64
			record.RawDocumentID = &value
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *Store) RecentSleepSessions(ctx context.Context, provider string, limit int) ([]SleepSession, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, local_date, zone_offset, external_id, start_time, end_time,
			duration_minutes, time_in_bed_minutes, efficiency_percent, is_nap,
			stages_json, metrics_json, raw_document_id
		FROM sleep_sessions
		WHERE provider = ?
		ORDER BY start_time DESC
		LIMIT ?
	`, provider, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent sleep sessions: %w", err)
	}
	defer rows.Close()

	var out []SleepSession
	for rows.Next() {
		var session SleepSession
		var duration sql.NullInt64
		var timeInBed sql.NullInt64
		var efficiency sql.NullFloat64
		var isNap int
		var stages string
		var metrics string
		var rawDocumentID sql.NullInt64
		if err := rows.Scan(
			&session.Provider,
			&session.LocalDate,
			&session.ZoneOffset,
			&session.ExternalID,
			&session.StartTime,
			&session.EndTime,
			&duration,
			&timeInBed,
			&efficiency,
			&isNap,
			&stages,
			&metrics,
			&rawDocumentID,
		); err != nil {
			return nil, fmt.Errorf("scan recent sleep session: %w", err)
		}
		if duration.Valid {
			value := int(duration.Int64)
			session.DurationMinutes = &value
		}
		if timeInBed.Valid {
			value := int(timeInBed.Int64)
			session.TimeInBedMinutes = &value
		}
		if efficiency.Valid {
			value := efficiency.Float64
			session.EfficiencyPercent = &value
		}
		session.IsNap = isNap != 0
		session.Stages = json.RawMessage(stages)
		session.Metrics = json.RawMessage(metrics)
		if rawDocumentID.Valid {
			value := rawDocumentID.Int64
			session.RawDocumentID = &value
		}
		out = append(out, session)
	}
	return out, rows.Err()
}
