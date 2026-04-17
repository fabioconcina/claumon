## Added

- **Claude Opus 4.7 pricing** — added pricing for the new Opus 4.7 model ($5/$25 per MTok). Unknown opus models now fall back to 4.7 pricing.

## Improved

- **Atomic pricing cache writes** — cache file is now written via temp file + rename to prevent corruption on crash.
- **File close error handling** — updater now properly checks `Close()` errors when writing binaries, preventing silent failures.
- **Consistent log format** — all log messages now use `[tag]` prefix format (e.g. `[startup]`, `[shutdown]`).

## Refactored

- Extracted helpers in memory discovery and session parsing to reduce duplication.
- Added `SendJSON` convenience method to SSE broker, replacing repeated marshal+broadcast boilerplate.
- Simplified memory API handlers by removing redundant nil guards.
- Config loader now derives paths from defaults instead of recomputing `$HOME`.
