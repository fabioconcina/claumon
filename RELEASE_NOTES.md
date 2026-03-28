## New

- **Auto-memory health scores** — per-file grading (freshness, structure, specificity, connectedness) with letter grades and improvement suggestions

## Changed

- **Removed cross-project edges from memory graph** — the entity-based linking (SSH hosts, binary paths) was noise, not signal

## Fixed

- **Flaky `TestHandleHistory`** — use relative date instead of hardcoded value that drifts out of the query window
