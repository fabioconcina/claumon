## Fixes

- **Forecast 80% CI was systematically under-spread.** Live monitoring
  showed only ~25% of session outcomes falling inside the displayed band.
  The §5 calibration regression was discarding its quadratic coefficient,
  which carries the historical-average rate variance; v1.1 retains it and
  uses it as a floor on the per-forecast rate uncertainty. CIs widen
  accordingly, most visibly at long horizons.

  Forecast model version → `v1.1`. The v1.0 spec is preserved under
  [`internal/forecast/archive/v1.0/`](internal/forecast/archive/v1.0/);
  the math change is documented in
  [`internal/forecast/CHANGELOG.md`](internal/forecast/CHANGELOG.md).

## Diagnostics

- `claumon diagnostics` now prints a "Spread sanity" block: mean squared
  error of `F`, mean predicted variance, an underspread ratio
  (`1.0` = calibrated, `> 1` = bands too narrow), and the components feeding
  the new `EffectiveRateVar`. Use it after a few days of v1.1 data to check
  the fix actually landed for your usage pattern.

## Docs

- Model spec §3 picks up a paragraph documenting the Brownian-motion
  simplification (utilization is monotone, BM isn't) and the conditions
  under which the bias matters.
