## Bug fixes

- **Daily history is now a continuous calendar.** The "Daily Tokens (14 days)"
  and cost charts skipped days with no Claude usage, packing active days
  together so the date axis wasn't continuous. `GetHistory` now returns the full
  window of N days ending today, zero-filling idle days so they render as empty
  bars with their date labels. This also corrects a latent off-by-one where the
  window spanned N+1 days.

- **Stopped a self-inflicted rate-limit storm after a rejected token.** When the
  usage API rejected the access token with a 401, the poller marked auth expired
  but the next credential reload reset it to OK on the still-future local
  `ExpiresAt`, so polling resumed, drew another 401, and looped every 30s until
  the API returned 429. The auth provider now remembers the rejected token and
  stays expired until a genuinely different token is written to disk (i.e.
  Claude Code refreshed it), so the poller waits quietly instead of hammering
  the API.
