Initial release of claumon — a minimal Claude Code dashboard.

## Features

- **Rate limit gauges** — session (5h) and weekly utilization with reset countdowns, per-model quotas, extra usage credits
- **Token usage** — per-session breakdown of input/output/cache tokens with estimated API cost
- **Session browser** — active sessions table with detail view showing full message timeline
- **Historical trends** — 14-day charts and 24-hour activity heatmaps backed by SQLite
- **Memory browser** — search, filter, and inspect all memory files with staleness alerts
- **Memory graph** — interactive visualization of cross-project relationships
- **Live updates** — real-time via SSE, no polling or manual refresh

## Install

Single binary, zero config. Download, run, open `http://localhost:3131`.

Reads credentials from `~/.claude/.credentials.json` or your OS credential store. If missing, session tracking still works — only API usage gauges are unavailable.
