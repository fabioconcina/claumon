## Fixed

- **Silent auth failures now visible in dashboard** — when OAuth tokens expire, the UI shows an auth banner and "Stale — poll failing" on usage gauges instead of silently displaying stale 0% data for hours.
- **Respect Retry-After header on 429 rate-limit responses** — avoids hammering the API when rate-limited.
- **Service restart no longer kills itself on Windows.**

## Improved

- Dashboard polls auth status every 60s as a fallback, so persistent failures are always surfaced even after page refresh or SSE reconnect.
- `/api/usage` response now includes `last_poll_at` and `poll_error` fields for programmatic staleness detection.
