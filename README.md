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

Then open [http://localhost:18080](http://localhost:18080).

## Development checks

```bash
make test
make lint
```

The frontend build can be validated with:

```bash
pnpm --dir frontend install
pnpm --dir frontend build
```

## Release flow

The repo now includes a lightweight GitHub Actions + GoReleaser release path:

- `.github/workflows/ci.yml` runs Go tests on Linux and macOS, builds the frontend, and runs `golangci-lint`
- `.github/workflows/release.yml` runs on semver tags and publishes GitHub release archives plus checksums through GoReleaser
- `.goreleaser.yml` builds `somascope` for `darwin` (`amd64`, `arm64`), `linux` (`amd64`, `arm64`), and `windows` (`amd64`)

Recommended release command:

```bash
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

The workflow also accepts bare tags such as `0.1.0`, but `v0.1.0` is the cleaner default.

## Private repo note

Keeping `somascope` private is compatible with GitHub Actions and GoReleaser for private releases.

The Homebrew story can wait until the repo is public:

- public GitHub Releases are the simplest future source for a tap
- private-only distribution is better handled as direct release downloads for now

## Notes

- The Go scaffold builds with only the standard library.
- The frontend workspace is scaffolded but dependencies are not installed in this pass.
- The embedded UI is currently a placeholder shell that exposes the agreed v1 boundaries.
