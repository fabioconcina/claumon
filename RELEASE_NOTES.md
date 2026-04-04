## Added

- **Context length in session detail** — session detail view now shows the current context window size (input + cache read + cache create tokens)
- **Cache read/write breakdown in today card** — token detail line splits cache into CRead and CWrite instead of a single combined value

## Fixed

- **SSE reconnect on server restart** — EventSource now properly reconnects with exponential backoff when the server restarts, and refreshes all data on reconnect
- **Graph label overlap** — memory graph uses more vertical space and truncates long project labels to prevent overlap
- **New project directories not detected** — file watcher now watches the projects directory itself, so new project dirs are picked up immediately and their existing session files are scanned
