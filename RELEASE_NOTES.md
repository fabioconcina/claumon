## Added

- **Auto-reload credentials on token expiry** — credentials are now reloaded from disk/OS store when the OAuth token expires, so the dashboard recovers automatically after a Claude Code session refreshes the token. No more restarts needed.
- **Auth status banner** — yellow banner appears when auth is expired, disappears when credentials are refreshed
- **Auth status API** — new `GET /api/auth/status` endpoint and `auth_status` field in `/api/info`
- **Cache tokens in today summary** — token detail line now shows cache read+create totals
