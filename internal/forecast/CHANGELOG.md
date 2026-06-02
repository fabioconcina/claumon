# Forecast model changelog

Each entry below corresponds to a value of `forecast.ModelVersion` in the Go
code. When the model changes in a way that shifts forecast distributions,
ETAs, or calibration semantics, the prior `MODEL.tex` (and its PDF) moves to
`archive/v<old>/`, the version constant bumps, and a new entry lands here.

Bug fixes that bring the implementation back in line with the existing spec
do **not** bump the model version — only changes to the spec itself do.

## v2.0 - 2026-06-02 (current)

Replaces the Brownian path law with a **Gamma process** (a non-decreasing Lévy
subordinator), so simulated utilization is monotone non-decreasing - the
physically correct shape for a cumulative quota gauge. This is the upgrade v1.x
flagged in `MODEL.tex` (the generative-model and limitations notes) as the
natural next step.

- **Positive-only increments.** In the Monte Carlo, the per-path rate and
  each per-step increment are now Gamma draws (`internal/forecast/subord.go`,
  Marsaglia-Tsang) matching the same first two moments the Brownian model used
  (mean `r·dt`, variance `σ_session²·dt` per step; rate variance floored by
  `bar_τ²`). Every draw is `>= 0`, so trajectories never decrease and the
  forecast-trajectory modal no longer dips.
- **CI from the MC, not a z-quantile.** `Run` now reads the 80% CI off the 10th
  and 90th percentiles of the MC terminal distribution. The lower edge sits at
  or above `u_now` by construction, so the old clip-to-`u_now` patch is gone and
  the interval is honestly right-skewed; the upper edge is still capped at 1.
  (On the worked example the displayed CI goes from the clipped symmetric
  `[30%, 75%]` to the skewed `[30%, 78%]`: the same lower floor, now reached
  honestly rather than by clipping a Gaussian tail that fell to 23%, with a
  slightly longer upper reach.)
- **Honest first-passage.** Monotone paths cross a threshold once and stay
  above, eliminating the "biased late" ETA artifact of Brownian paths that
  dipped and re-crossed.
- **Unchanged.** The point forecast `F = u_now + r_hat·Δt`, its moment spread
  `σ_F` (still reported as `sigma_pct`), the rate estimation (OLS + conjugate
  prior), and the calibration of `σ_session²` / `bar_τ²` all carry over
  verbatim. `ProjectForecast` is retained as the moment helper used by the
  `benchtools` diagnostics/bench harness.

Scope note: this change targets realism (monotone paths, an honest lower
floor), not a chase for a better score. The `benchtools` bench/diagnostics
harness was updated in lockstep to score the actual v2.0 distribution instead
of a moment-matched Gaussian: `bench.Predictive` now carries the Monte Carlo
terminal sample, scored with an unbiased sample CRPS and empirical quantiles.
Scoring the real interval surfaced that the 80% CI under-covers on idle-heavy
real data - driven by the point forecast's pre-existing over-prediction on
abandoned sessions, not by the interval shape (the honest floor no longer masks
it). That is a centering problem, an instance of the abandonment limitation in
the spec's limitations section, not a reason to widen the band.

## v1.2 - 2026-05-30

Two changes, both informed by the out-of-sample benchmark added in
`internal/forecast/bench` (LOSO + temporal holdout, CRPS/pinball scoring,
synthetic golden regimes).

- **Calibration regression is now weighted.** The joint fit of `e_f²` on
  `[δ, δ²]` uses weights `w = 1/δ²` instead of plain OLS. `e_f²` is a squared
  error and so heteroskedastic (its variance grows steeply with horizon);
  unweighted OLS was dominated by the few long-horizon points and inflated
  `σ_session²` by ~8×. That over-predicted short-horizon spread and, through
  the §5 prior noise correction, over-subtracted until `tau0Sq` floored to ε,
  silently pinning the rate prior. The weighted fit calibrates the spread
  (in-sample underspread ratio 0.34 → 0.97) and revives the prior; `bar_τ²`
  comes from the same fit. Head-to-head on real exports: net-better CRPS and
  consistently closer-to-80% coverage, strongest on the higher-engagement
  device.
- **Confidence tag removed.** The `Low/Medium/High` tag (former §9), its API
  field (`confidence`), the `Config.HighNEff`/`MediumNEff` knobs, and the UI
  badge are all dropped. The tag scored the amount of recent-slope evidence
  (`N_eff`), which the benchmark showed is uncorrelated — often
  anti-correlated — with forecast quality (a precise recent slope is frequently
  a confidently biased one). The 80% CI already conveys predictive uncertainty;
  a miscalibrated badge only added noise. The output set in §1 loses the tag.

## v1.1 - 2026-05-28

Fixes systematic CI under-spread observed in the field (live coverage on the
80% CI was ~25% across all horizons, against a 80% target).

- **§4 spread:** rate-uncertainty term changes from `Δt²·τ_post²` to
  `Δt²·max(τ_post², bar_τ²)`. The historical-average rate variance `bar_τ²`
  was previously computed during calibration and discarded; now it is kept
  on `Calibration.BarTauSq` and used as a floor.
- **§5 calibration:** the regression is unchanged, but `b_hat` is no longer
  discarded - it becomes `bar_τ²`. Added a paragraph explaining why the
  conjugate `τ_post²` under-spreads when assumption A1 (constant within-window
  rate) is violated, and why `bar_τ²` is the right scale for the rest of the
  session.
- **§6 MC:** trajectory rate draws use the floored standard deviation
  `sqrt(max(τ_post², bar_τ²))` to keep the MC consistent with §4.
- **Diagnostics:** `Diagnostics` now reports `MeanE2`, `MeanPredVar`,
  `UnderspreadX` (= MeanE2 / MeanPredVar; 1.0 is calibrated), `BarTauSq`,
  `MeanTauPostSq`, and `MeanEffRateVar`. `Report()` prints a "Spread sanity"
  block.

## v1.0 - 2026-05-28

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
