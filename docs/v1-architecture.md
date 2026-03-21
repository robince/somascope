# Somascope V1 Architecture

## Goals

- Keep wearable data on the user's device.
- Make the app easy to distribute as a local binary or desktop wrapper.
- Start with Fitbit and Oura in `byo` OAuth mode.
- Normalize daily summaries without losing provider-native fields.
- Make export a first-class feature.

## Non-goals for v1

- Shared cloud-hosted data storage
- Multi-user support
- Brokered managed OAuth
- Minute-level or intraday-first analytics
- DuckDB as a primary store

## Auth modes

### `byo`

The local app stores provider app credentials, tokens, and sync state on-device.

### Future `brokered`

A hosted broker may later own shared provider app credentials and refresh flow while still keeping normalized data local. The rest of the app should not depend on that distinction.

## Storage model

V1 keeps a single local data root under `~/.somascope/`.

Reserved paths:

- `config.json`
- `somascope.db`
- `exports/`
- `raw/`
- `logs/`

## Canonical entities

V1 uses an Open Wearables-inspired model but adds a first-class daily fact layer.

- `connections`
- `provider_credentials`
- `sync_state`
- `daily_facts`
- `sleep_sessions`
- `metric_definitions`
- `raw_documents`

## Canonical export targets

### Raw export

- provider-native JSON payloads as fetched

### Structured export

- canonical JSONL
- canonical CSV
- later Parquet

Structured rows should preserve:

- `provider`
- `source_device`
- `local_date`
- `zone_offset`
- `metric_code`
- `value`
- `unit`
- `external_id`
- `provenance`
- `raw_document_ref`

## UI direction

Use the dense, analytical `agentsview` chart language rather than the more bespoke `thymostat` visual style.

V1 should prioritize:

- daily activity blocks
- daily sleep summary cards
- source-aware exports
- simple metric toggles

## Implementation notes

- Go server owns the API and SPA fallback.
- Frontend is developed separately and embedded for distribution.
- Initial scaffold may use placeholder embedded assets while the frontend is built out.
