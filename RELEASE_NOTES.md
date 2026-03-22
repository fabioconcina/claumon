## New

- **Wasted token detection** — sessions are flagged when cache efficiency is low (<50% reuse on large sessions) or when lots of tokens are spent without any file edits
- **Cache efficiency metric** — `cache_read / (input + cache_read + cache_create)` properly accounts for expensive cache creation costs
- **Efficiency column** in sessions table with cache reuse % and waste badges
- **Billable-only toggle** on daily tokens chart to see just the cost-driving tokens (input, output, cache write)
- **DB pruning** with configurable retention policy
- **Keyboard shortcuts** for tab switching and memory search
- **`--open` flag** to auto-launch browser on start

## Improved

- Simplified daily tokens chart (single bar) with stacked billable breakdown on toggle
- Fixed indistinguishable Input/Cache Write legend colors
- Cache reuse % and file edit status shown in session detail panel
- Multiple rounds of tech debt reduction: deduplicated helpers, better error wrapping, mutex for memory cache

## Install

Single binary, zero config. Download, run, open `http://localhost:3131`.

Reads credentials from `~/.claude/.credentials.json` or your OS credential store. If missing, session tracking still works — only API usage gauges are unavailable.
