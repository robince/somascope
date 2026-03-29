# Next Steps

This document describes the recommended next implementation slice and the high-level stages after that.

## Immediate next step

Implement the first real provider import path end to end.

Recommended first target:

- Oura daily activity
- Oura daily readiness
- Oura sleep sessions

Why Oura first:

- it fits the current schema cleanly
- it exercises both `daily_records` and `sleep_sessions`
- it makes the canonical export endpoint immediately useful
- it gives real data to shape dashboard queries before adding more providers

## Stage 1: Oura import slice

Goal:

Take locally configured Oura credentials and turn them into real stored records and exports.

Suggested tasks:

1. Add provider configuration validation
   - verify required local Oura settings exist
   - return clear errors if credentials are incomplete

2. Add Oura client package
   - authorization/token handling hooks or placeholder token import path
   - HTTP client for:
     - daily activity
     - daily readiness
     - sleep

3. Add normalization layer
   - provider payload -> `raw_documents`
   - daily activity -> `daily_records` with `record_kind = "daily_activity"`
   - daily readiness -> `daily_records` with `record_kind = "daily_readiness"`
   - sleep -> `sleep_sessions`

4. Add a minimal sync endpoint
   - `POST /api/v1/providers/oura/sync`
   - start with a simple date range or recent-days sync

5. Add minimal read endpoints
   - recent `daily_records`
   - recent `sleep_sessions`

6. Update frontend
   - add a simple “sync now” action
   - show recent Oura daily records
   - show recent sleep sessions

Definition of done:

- a local user can configure Oura credentials
- trigger a sync
- see real records persisted locally
- export canonical JSONL/CSV containing those records

## Stage 2: Fitbit import slice

Goal:

Add Fitbit daily summaries and sleep in the same local-first pattern.

Suggested tasks:

1. Add Fitbit client
2. Fetch daily activity and sleep payloads
3. Normalize into:
   - `raw_documents`
   - `daily_records`
   - `sleep_sessions`
4. Reuse the same export surface
5. Add minimal provider-specific read UI if needed

Notes:

- Fitbit should follow the same storage/export structure as Oura where possible
- avoid inventing a different schema per provider

## Stage 3: Dashboard read APIs

Goal:

Turn stored local data into a small set of stable view endpoints for the frontend.

Suggested endpoints:

- `GET /api/v1/dashboard/summary`
- `GET /api/v1/dashboard/daily-records`
- `GET /api/v1/dashboard/sleep-sessions`

Initial focus:

- date range filters
- provider filters
- recent summaries
- enough structure for daily blocks and summary cards

## Stage 4: Better exports

Goal:

Make exports genuinely useful for non-technical and technical users.

Tasks:

1. Add raw export endpoint
   - provider-native JSON payload export

2. Expand canonical export metadata
   - provenance
   - source device
   - local date / timezone
   - raw document references

3. Add export UX
   - provider filter
   - date range filter
   - format selection

Possible later work:

- Parquet export
- packaged export bundles

## Stage 5: Intraday support

Not for immediate implementation, but should follow this shape:

- add `timeseries_points` as a separate long-format table
- keep `daily_records` and `sleep_sessions` as the source-of-truth daily/session layers

Candidate data:

- Oura 5-minute heart rate
- Fitbit intraday heart rate
- Fitbit intraday steps

This should not be stored inside `daily_records.summary_json`.

## Stage 6: Analytical projection

Only do this after real data and real queries exist.

If metric-centric or cross-provider analytics become important:

- add a relational `daily_metrics` projection
- or add a derived DuckDB/Parquet analytical layer

Do not prematurely replace the current source-of-truth model with a columnar-first design.

## Suggested sequencing

Recommended order:

1. Oura import
2. minimal read APIs
3. frontend display of real data
4. raw export
5. Fitbit import
6. dashboard refinement
7. intraday support
8. analytical projection if needed

## What not to do next

Avoid these as the immediate next step:

- broad auth abstraction before a real provider import exists
- brokered shared OAuth mode
- DuckDB-first rewrite
- multi-user support
- a large generalized metrics layer before real data access patterns are known
