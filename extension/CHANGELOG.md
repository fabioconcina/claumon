# Changelog

## 0.2.0 - 2026-06-19

### Added

- **Forecast-aware status bar.** The badge now reacts to where you're _projected_ to land, not just current usage: it turns yellow when the forecast projects you'll exceed the limit before the window resets, even while current usage is still low. A flame icon (replacing the usual pulse) shows whenever the trajectory is still working against you, so a red-and-flame badge ("high and still climbing") reads differently from red-and-pulse ("high, but you've eased off"). Background severity is the worst of current usage and the forecast; the icon tracks the forecast independently.

## 0.1.0 - 2026-06-17

First release. A thin VS Code client for a running [claumon](https://github.com/fabioconcina/claumon) server.

### Added

- **Status bar item** showing live session (or weekly) usage, updated over claumon's SSE stream. When a forecast is available it also shows the projected percentage at reset, e.g. `45%->72% session`. Turns yellow at >= 75% and red at >= 90%.
- **Hover tooltip** with the session/weekly breakdown, weekly Opus quota, reset times, and forecast detail (projected % with its 80% confidence interval and threshold ETA).
- **Dashboard panel** (`Claumon: Open Dashboard`) embedding the live claumon web UI, with an offline state and Retry. Only loopback hosts are embedded.
- **Resilient connection**: auto-reconnect with exponential backoff, a graceful offline state, and live response to settings changes.
- **Commands**: `Claumon: Open Dashboard`, `Claumon: Reconnect`.
- **Settings**: `claumon.host` and `claumon.port` (machine-scoped) and `claumon.statusBar.metric` (`session` or `weekly`).

### Notes

- Client only: it connects to a claumon server you are already running (default `localhost:3131`) and does not start or bundle claumon.
- **Zero runtime dependencies**; compiled with `tsc`. Requires VS Code 1.85+.
