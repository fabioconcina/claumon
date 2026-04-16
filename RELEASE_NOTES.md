## Fixed

- **Skip API calls when auth is expired** — when OAuth credentials expire overnight, the poller now checks auth status locally (every 30s) instead of hitting the API. This avoids burning through rate limits with guaranteed-to-fail requests and eliminates the 1-hour 429 backoff that would follow when auth recovered.
