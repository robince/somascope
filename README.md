# somascope

`somascope` is a local-first wearables dashboard focused on private local storage, simple export, and a future path to flexible dashboards.

## V1 shape

- Local Go server
- Embedded static frontend for distribution
- Separate frontend workspace for local development
- Single-user app data in `~/.somascope/`
- `byo` OAuth mode first: users provide their own Fitbit/Oura app credentials
- Daily-summary-first canonical model with raw export and structured export

## Layout

- `cmd/somascope`: Go entrypoint
- `internal/config`: data-dir and runtime configuration
- `internal/server`: HTTP server and API surface
- `internal/web`: embedded assets
- `frontend`: Svelte/Vite frontend scaffold
- `docs`: short design specs

## Quick start

```bash
make dev
```

Then open [http://127.0.0.1:8080](http://127.0.0.1:8080).

## Notes

- The Go scaffold builds with only the standard library.
- The frontend workspace is scaffolded but dependencies are not installed in this pass.
- The embedded UI is currently a placeholder shell that exposes the agreed v1 boundaries.
