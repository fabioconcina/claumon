## Watcher CPU fix, forecast modal at the limit, Windows test fix

- **Watcher no longer pegs a core on an active session.** A continuously
  written session JSONL emitted Write events many times per second; the old
  per-path cooldown still let one through every 500ms, and each dispatch
  re-parsed every session file. Replaced with a trailing-edge debouncer
  (fires 750ms after writes go idle, capped at a 3s max-wait so a perpetually
  hot file still refreshes). Measured on an active session: ~103% of one core
  down to ~13%.

- **The forecast modal no longer goes blank at 100% usage.** At the limit
  there's no headroom to simulate, so the sample endpoint used to return a
  bare "unavailable" that rendered as the generic "No forecast available yet."
  empty state - looking broken right when the gauge is pinned. It now reports
  an explicit at-limit reason and the modal shows "Limit reached - already at
  100%. Nothing left to forecast until reset."

- **Windows test fix.** `TestBuildGraphIndexLinks` hardcoded forward-slash
  paths, but `BuildGraph` normalizes separators via `filepath.Dir/Join`, so
  the test failed on Windows only (production was unaffected). The test now
  builds OS-native paths.

No forecast-model, pricing, or storage changes.
