## Memory

- **Delete individual memory files from the dashboard.** Each memory note now
  has a `delete` button next to `edit`, with a confirmation prompt. Deletion is
  restricted to individual memory files: `CLAUDE.md`, rule files, and the
  `MEMORY.md` index are protected, both in the UI and in the API
  (`POST /api/memories/delete`), which only removes a path that resolves to a
  known `memory-file`. The matching pointer line is also pruned from the
  sibling `MEMORY.md` index, so a delete no longer leaves a dangling link or
  triggers staleness alerts.

## Dashboard

- **14-day totals on the history charts.** The "Daily Tokens" and "Equiv. API
  Cost" charts now show a running total for the whole 14-day window in their
  header (e.g. `Total 44.3M`, `Total $452.18`). The tokens total tracks the
  "Billable only" toggle, so it reflects exactly what the bars represent.
