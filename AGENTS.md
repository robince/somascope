# AGENTS.md

This file describes the current shape of `somascope`, the design goals it is optimizing for, and the constraints future work should preserve unless there is an explicit decision to change them.

## Product intent

`somascope` is a local-first wearables dashboard.

The core goals are:

- keep user wearable data on the user's own device
- make setup and distribution simple
- support Fitbit and Oura first
- provide explicit export paths so users can inspect, analyze, and reuse their own data
- start with dense, analytical dashboard views rather than a highly ornamental wellness UI

## Current v1 position

The project is still in the early scaffold stage, but several architectural choices are already in place and should be treated as the default direction:

- single-user local app
- Go backend
- Svelte frontend
- SQLite local store
- BYO provider credentials first
- daily-summary-first canonical model
- raw export and structured export as first-class features

## Non-goals for v1

Do not expand v1 into these unless there is an explicit change of plan:

- multi-user support
- cloud-hosted shared storage
- managed shared OAuth broker as the primary auth mode
- intraday-first or minute-level-first analytics
- making DuckDB the source of truth

## Architecture

## Backend

The Go app is responsible for:

- config loading
- local directory layout
- SQLite bootstrap
- schema migrations
- HTTP API
- serving the embedded frontend build
- future provider sync/import logic

Relevant files:

- [main.go](/Users/robince/code/somascope/cmd/somascope/main.go)
- [config.go](/Users/robince/code/somascope/internal/config/config.go)
- [server.go](/Users/robince/code/somascope/internal/server/server.go)
- [store.go](/Users/robince/code/somascope/internal/store/store.go)

## Frontend

The Svelte frontend is currently a thin local UI client.

In development it runs via Vite and proxies `/api` to the Go server.
For distribution it should be built into `frontend/dist` and copied into `internal/web/dist` for embedding.

Relevant files:

- [App.svelte](/Users/robince/code/somascope/frontend/src/App.svelte)
- [main.ts](/Users/robince/code/somascope/frontend/src/main.ts)
- [vite.config.ts](/Users/robince/code/somascope/frontend/vite.config.ts)
- [embed.go](/Users/robince/code/somascope/internal/web/embed.go)

## Dev modes

### Backend-only

```bash
make dev
```

This serves the Go API and the embedded stub page if the frontend build has not been embedded.

### Frontend + backend

Terminal 1:

```bash
make dev
```

Terminal 2:

```bash
cd frontend
pnpm install
pnpm dev
```

The frontend dev server proxies `/api` to the Go backend.

## Local storage layout

Default local data root:

- `~/.somascope/`

Reserved paths:

- `config.json`
- `somascope.db`
- `exports/`
- `raw/`
- `logs/`

## Current canonical data model

The current model deliberately separates raw source payloads, daily summaries, and interval/session records.

- `raw_documents`
  - provider-native JSON payloads as fetched
- `daily_records`
  - one structured normalized daily summary per provider, local date, and record kind
- `sleep_sessions`
  - one structured sleep interval per sleep episode
- `connections`
  - provider OAuth connection state
- `provider_credentials`
  - local BYO app credentials
- `sync_state`
  - provider/entity sync cursors and watermarks

Important distinction:

- `daily_records` is not one row per metric
- it is one row per structured daily summary record

Example:

- Oura `daily_activity` for one day: one `daily_records` row
- Oura `daily_readiness` for one day: one `daily_records` row
- Oura overnight sleep: one `sleep_sessions` row

If later analytics become metric-centric, the likely next layer is a separate long-format `daily_metrics` or `timeseries_points` projection, not replacing the source-of-truth daily/session model.

## Naming guidance

Prefer names that describe the data shape:

- use `daily_records` for structured daily provider summaries
- use `sleep_sessions` for interval data
- reserve names like `daily_metrics` for future long-format one-row-per-metric data if it is actually introduced

## Auth direction

### Current

V1 uses `byo` mode:

- users enter provider client ID and client secret locally
- secrets remain local
- the app stores provider settings on-device

### Future

A future `brokered` mode may exist, but the rest of the app should not be tightly coupled to it.

If that mode is added later, it should preserve:

- local normalized data storage
- local export capability
- minimal dependence of the UI on broker-specific behavior

## Export direction

Exports are a core feature, not a nice-to-have.

The current intended export surfaces are:

- raw JSON
- canonical JSONL
- canonical CSV
- possibly Parquet later

The backend already has the first canonical export endpoint:

- `GET /api/v1/export/canonical?format=jsonl|csv`

## Dashboard direction

Prefer the analytical style of `agentsview` rather than the more bespoke `thymostat` chart architecture.

Prioritize:

- daily activity blocks
- daily summary inspection
- sleep summaries
- source-aware exports
- simple metric toggles

## Immediate implementation priorities

The next real product step is not more scaffolding. It is one end-to-end import path for a real provider.

The current recommended next slice is:

- Oura daily activity import
- Oura daily readiness import
- Oura sleep import

This should write into:

- `raw_documents`
- `daily_records`
- `sleep_sessions`

And then expose minimal read APIs for recent records so the frontend can render real data.

## Decision bias

When choosing between a simple local-first implementation and a more elaborate generalized one:

- prefer the simpler local-first implementation
- prefer explicit exportability over abstract elegance
- prefer provider-shaped correctness over prematurely generalized analytics layers
- prefer incremental projections later over forcing the write model to serve all future read patterns now
