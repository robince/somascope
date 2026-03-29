package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestOpenAppliesInitialMigrations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "somascope.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	version, err := store.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if version != 3 {
		t.Fatalf("expected schema version 3, got %d", version)
	}
}

func TestCanonicalExportRowsIncludesRecordsAndSleep(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "somascope.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.UpsertDailyRecord(ctx, DailyRecord{
		Provider:     "oura",
		RecordKind:   "daily_activity",
		LocalDate:    "2026-03-20",
		ZoneOffset:   "+01:00",
		SourceDevice: "oura-ring-4",
		ExternalID:   "activity-1",
		Summary:      json.RawMessage(`{"steps":12345,"active_calories_kcal":512}`),
	}); err != nil {
		t.Fatalf("upsert daily record: %v", err)
	}

	duration := 438
	timeInBed := 462
	efficiency := 94.8
	if err := store.InsertSleepSession(ctx, SleepSession{
		Provider:          "oura",
		LocalDate:         "2026-03-20",
		ZoneOffset:        "+01:00",
		ExternalID:        "sleep-1",
		StartTime:         "2026-03-19T22:58:00+01:00",
		EndTime:           "2026-03-20T06:16:00+01:00",
		DurationMinutes:   &duration,
		TimeInBedMinutes:  &timeInBed,
		EfficiencyPercent: &efficiency,
		Stages:            json.RawMessage(`{"deep_minutes":92,"rem_minutes":88}`),
		Metrics:           json.RawMessage(`{"avg_heart_rate_bpm":54}`),
	}); err != nil {
		t.Fatalf("insert sleep session: %v", err)
	}

	rows, err := store.CanonicalExportRows(ctx)
	if err != nil {
		t.Fatalf("canonical export rows: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 export rows, got %d", len(rows))
	}
	if rows[0].RecordType != "daily_record" || rows[1].RecordType != "sleep_session" {
		t.Fatalf("unexpected row types: %+v", rows)
	}
}

func TestRawExportRowsFiltersByProvider(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "somascope.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if _, err := store.UpsertRawDocument(ctx, RawDocument{
		Provider:     "oura",
		DocumentKind: "daily_activity",
		ExternalID:   "activity-1",
		LocalDate:    "2026-03-20",
		RequestPath:  "/v2/usercollection/daily_activity",
		RequestQuery: "start_date=2026-03-20&end_date=2026-03-21",
		RequestStart: "2026-03-20",
		RequestEnd:   "2026-03-21",
		Payload:      json.RawMessage(`{"id":"activity-1","steps":12345}`),
		FetchedAt:    "2026-03-20T10:00:00Z",
		DocumentKey:  "daily_activity:activity-1",
	}); err != nil {
		t.Fatalf("insert oura raw document: %v", err)
	}

	if _, err := store.UpsertRawDocument(ctx, RawDocument{
		Provider:     "fitbit",
		DocumentKind: "sleep",
		ExternalID:   "sleep-1",
		LocalDate:    "2026-03-20",
		Payload:      json.RawMessage(`{"logId":"sleep-1"}`),
		FetchedAt:    "2026-03-20T11:00:00Z",
		DocumentKey:  "sleep:sleep-1",
	}); err != nil {
		t.Fatalf("insert fitbit raw document: %v", err)
	}

	rows, err := store.RawExportRows(ctx, "oura")
	if err != nil {
		t.Fatalf("raw export rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 raw export row, got %d", len(rows))
	}
	if rows[0].RecordType != "raw_document" {
		t.Fatalf("expected raw_document record_type, got %q", rows[0].RecordType)
	}
	if rows[0].Provider != "oura" || rows[0].DocumentKind != "daily_activity" {
		t.Fatalf("unexpected raw export row: %+v", rows[0])
	}
	if rows[0].RequestPath != "/v2/usercollection/daily_activity" {
		t.Fatalf("expected request_path to round-trip, got %q", rows[0].RequestPath)
	}
}
