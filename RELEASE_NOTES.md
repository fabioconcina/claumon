## Fix: forecast popup now reports the same numbers as the gauge

- **The trajectory popup showed a different 80% CI (and ETA) than the gauge
  line.** Three things were pulling them apart. (1) The popup footer was still
  reporting `ProjectForecast`'s symmetric Gaussian `mean ± z·σ` interval instead
  of the v2.0 monotone Monte Carlo terminal quantiles the gauge uses. (2) The
  popup simulated against an 80% threshold while the gauge uses 100%, and since
  the threshold is folded into the Monte Carlo seed, the two drew different
  sample paths and therefore different intervals (this also made the popup say
  "no forecast" once usage passed 80%). (3) The popup re-ran the whole simulation
  at open time, a different instant than the poll that fed the gauge, so even
  matched inputs drifted by sampling noise. Now both surfaces forecast against
  the same threshold, and the popup's headline (projected %, 80% CI, ETA) reuses
  the gauge's already-computed forecast rather than re-simulating it, so the two
  always show identical numbers. The trajectory fog and histogram are still a
  fresh simulation for the visualization.

## Pricing: Claude Opus 4.8

- **Added `claude-opus-4-8` to the pricing table.** Same tier as Opus
  4.5/4.6/4.7 ($5 / $25 per million input/output tokens). Previously Opus 4.8
  sessions fell through to the `claude-opus-4-7` fallback, which carries the same
  price, so costs were already correct; the model now resolves to its own entry
  explicitly, and the "latest known opus" fallback points at 4.8. Pricing lives
  in [`pricing.json`](pricing.json), mirrored to the embedded fallback in
  [`internal/pricing/embedded.json`](internal/pricing/embedded.json).
