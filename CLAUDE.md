# claumon

Real-time Claude Code dashboard — single binary, zero config. Monitors API usage, token costs, sessions, and memory files via a local web UI on port 3131.

## Build & Test

```bash
make build            # builds ./claumon with version from git tags
make build-benchtools # build including the dev-only bench/diagnostics subcommands
make test             # go test -v -race -count=1 ./...
make vet              # go vet ./...
./claumon --open      # run and open browser
```

The `bench` and `diagnostics` subcommands (forecast model benchmarking and
calibration replay) are gated behind the `benchtools` build tag, so they are
excluded from release binaries. Use `make build-benchtools` (or
`go build -tags benchtools .`) to enable them locally.

## Architecture

Single `main.go` orchestrates 4 goroutines (SSE broker, usage API poller, file watcher, pricing refresh) and an HTTP server. All packages live under `internal/`:

- **api** — Claude OAuth usage API client with exponential backoff
- **auth** — Multi-platform credential loading (macOS Keychain, Linux secret-service, Windows Credential Manager)
- **parser** — Session JSONL discovery and token/cost aggregation
- **pricing** — Layered pricing: embedded JSON → 24h cache → GitHub remote → config overrides
- **memory** — Memory file discovery, graph building, staleness detection, consolidation scoring
- **server** — HTTP routes, handlers, SSE broker
- **store** — SQLite (WAL mode) for usage snapshots and daily aggregates
- **watcher** — fsnotify-based file watcher with 500ms debounce

Frontend is a single `web/index.html` embedded via `//go:embed`. No build step, no external JS deps.

`extension/` is a separate VS Code extension (TypeScript): a thin client that reads the same `/api/usage` + SSE feed and renders usage/forecast in the status bar plus an embedded dashboard panel. It has its own Node toolchain, version, and lockfile, and **zero runtime dependencies** (compiled with `tsc`, not bundled). It is not part of the Go build and is published separately (see the `ext-v*` tag in `.github/workflows/publish-extension.yml`).

## Conventions

- Log format: `log.Printf("[tag] message", ...)` with tags like `[poll]`, `[watcher]`, `[memory]`, `[auth]`, `[backfill]`, `[aggregate]`
- Errors: return errors, don't panic. Fatal only in `main()` for startup failures (DB open).
- JSON API responses always use `writeJSON`/`writeJSONError` helpers. Return empty slices (not null) for empty collections.
- Tests: table-driven where applicable, `_test.go` alongside source files. Race detector is on.
- Version injected via `-ldflags "-X main.version=..."` at build time.
- Cross-platform: no CGO (`CGO_ENABLED=0`), builds for darwin/linux/windows × amd64/arm64 via goreleaser.
