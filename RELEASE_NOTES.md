## Forecasts on the rate-limit gauges

Each gauge now shows a projection of where utilization will land at reset, with an 80% credible band, an ETA to threshold (default 100%), and a `LOW`/`MED`/`HIGH` confidence pill. This is an *empirical Bayes* forecast: the prior on the rate and the path-noise variance are both estimated from past data and plugged in to a posterior update, rather than being marginalized under a hyperprior. The full spec lives at [`internal/forecast/MODEL.md`](internal/forecast/MODEL.md); the gist:

- **Rate posterior.** Inside the open window, utilization is modeled as `u(t) = u_now + r·t + W(t)`, with `r` an unknown per-hour rate and `W` Brownian path noise with per-hour variance `σ²`. The current rate is fit by OLS on the last 30 minutes of snapshots; that OLS slope and its standard error are treated as a Gaussian likelihood `r̂_OLS | r ~ N(r, SE²_OLS)`, then fused with a Gaussian empirical prior on `r` (mean `μ₀`, variance `τ₀²`) via the standard conjugate normal-normal update. The prior is refit daily from up to 200 completed past windows.

- **Closed-form projection.** Both the rate-uncertainty and path-noise pieces are Gaussian, so projected utilization at reset is `F ~ N(u_now + r̂·ΔT, ΔT²·τ_post² + ΔT·σ²)`. The 80% credible interval is `F ± z₀.₉·σ_F`, clipped to `[0%, 100%]` for display. (Surfaced to users as "80% CI" for brevity.)

- **Path-noise calibration.** `σ²` is recovered by replaying the forecaster across past windows: at each replay point, the squared forecast error `e²` against the actual `u_final` is regressed against `[ΔT, ΔT²]` with no intercept. The linear coefficient is path noise; the quadratic coefficient absorbs rate-uncertainty contamination and is discarded. This is a method-of-moments estimator, then plugged into the posterior; the prior is refit once more with the noise correction applied to its sample variance.

- **Monte Carlo ETA.** For each threshold, 500 trajectories of the SDE are simulated at 5-minute steps, drawing one rate sample per trajectory from the posterior. The reported ETA is the median first-passage time with the 80% CI from the 10th/90th percentiles. If at least half the trajectories never cross before reset, the threshold is reported as unreachable (`p_inf` is exposed on the API payload).

- **Confidence tag.** Derived from effective sample size `n_eff = min(n_recent, τ₀²/SE_OLS² + N_sessions)`. `n_eff ≥ 50 → HIGH`, `≥ 15 → MEDIUM`, else `LOW`. Falls back to the prior alone when fewer than three recent snapshots exist.

The forecast is computed once per poll and shipped inside the existing `usage` SSE event; the same payload is also exposed at `GET /api/forecast` for pull-style clients. It is suppressed when fewer than two completed past windows exist, when the window has just reset, or when a drop in the recent snapshot series indicates a missed reset between polls.

## Fixes

- **Canonicalize `reset_at` timestamps at write time.** The Claude API returns `reset_at` recomputed as `now + remaining` on each poll, so the same nominal window drifts by hundreds of milliseconds across polls and occasionally straddles a minute boundary. Snapshots were being written with these drifting strings, so `GROUP BY session_reset_at` shattered every window into singletons and downstream aggregation lost track of session boundaries. Reset times are now rounded to the nearest minute on write, and a one-time idempotent migration canonicalizes existing rows on startup.
