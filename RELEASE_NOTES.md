## New

- **Auto-updating pricing** — model pricing is now loaded from a layered system: embedded defaults → local cache → remote fetch from GitHub (daily) → config overrides. No more stale prices when new models are released.
- **Fixed model pricing** — corrected Opus 4.6 ($15→$5/MTok input) and Haiku 4.5 ($0.80→$1/MTok input) rates, added all current and legacy models with full cache pricing (5-min and 1-hour tiers)
- **Unknown model warnings** — logs a warning when sessions use a model not in the pricing table, so you know when to update

## Improved

- **Usage API backoff** — exponential backoff (up to 10min) when the usage API is rate-limited, instead of hammering the endpoint
- **Cost display** — truncated to single decimal place for cleaner dashboard readability
- **Graph and memory fixes** — fixed graph node overlaps, broken memory links, session title extraction, and slow shutdown

## Install

Single binary, zero config. Download, run, open `http://localhost:3131`.

Reads credentials from `~/.claude/.credentials.json` or your OS credential store. If missing, session tracking still works — only API usage gauges are unavailable.
