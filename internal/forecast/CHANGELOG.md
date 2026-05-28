# Forecast model changelog

Each entry below corresponds to a value of `forecast.ModelVersion` in the Go
code. When the model changes in a way that shifts forecast distributions,
ETAs, or calibration semantics, the prior `MODEL.tex` (and its PDF) moves to
`archive/v<old>/`, the version constant bumps, and a new entry lands here.

Bug fixes that bring the implementation back in line with the existing spec
do **not** bump the model version — only changes to the spec itself do.

## v1.0 - 2026-05-28 (current)

Initial published model.

- **Rate.** Normal-Normal conjugate posterior on the per-window rate `r`,
  combining a Gaussian historical prior (`mu0, tau0Sq` fit per gauge) with
  OLS on snapshots in a 30-min recency window.
- **Forecast at reset.** Gaussian `u(T_reset) ~ N(F, σ_F²)` with
  `σ_F² = Δt²·τ_post² + Δt·σ_session²`. Displayed CI clipped at `[u_now, 1]`
  per the §5 monotonicity argument.
- **Path noise.** Brownian motion. Calibrated per-gauge via the joint
  regression `e_f² ~ a·δ + b·δ²` on historical replay residuals; only the
  linear coefficient is retained (`σ_session² := a`).
- **ETA.** Monte Carlo first-passage with K=500 trajectories, 5-min step.
  Reporting rules in §6 (three regimes by `p_∞`). RNG seeded deterministically.

See [MODEL.pdf](MODEL.pdf) for the full spec.
