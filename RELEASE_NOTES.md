## Forecast model → v1.2

- **The projection interval was systematically over-spread.** The §5
  calibration fit the path-noise variance with an unweighted regression of
  squared errors, which is heteroskedastic: the few long-horizon points
  dominated the fit and inflated the variance, so the 80% CI was wider than the
  data warranted (and the over-subtraction silently pinned the rate prior).
  v1.2 weights that regression by `1/Δt²`, which calibrates the spread and
  revives the prior. Out-of-sample, the CI lands consistently closer to its 80%
  target. The v1.1 spec is preserved under
  [`internal/forecast/archive/v1.1/`](internal/forecast/archive/v1.1/); the
  math is documented in
  [`internal/forecast/CHANGELOG.md`](internal/forecast/CHANGELOG.md).

- **Removed the Low/Medium/High confidence badge.** It scored how much recent
  data the forecast had, not how reliable the forecast actually was, so it
  could read "High" next to a wide interval and was uncorrelated with real
  accuracy. The 80% CI already conveys the uncertainty.

## New: `claumon bench`

- An out-of-sample benchmark for the forecast model: leave-one-session-out and
  temporal-holdout protocols, CRPS/pinball proper scoring with coverage, MAE,
  and bias breakdowns, segmented by engagement and horizon. Datasets are
  reproducible: frozen fixtures exported from any device's store
  (`claumon bench export --db ...`) and seeded synthetic regimes. A development
  and validation tool; it does not affect the dashboard.
