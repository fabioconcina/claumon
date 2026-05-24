# Rate-Limit Forecast Model

This document specifies the math behind `internal/forecast`. It is normative:
implementations must match the formulas here, and deviations are bugs unless
this document is updated.

## What we forecast

For each open rate-limit window (session, weekly, per-model weekly), the
forecaster reads the time series of utilization snapshots and produces:

1. A point estimate $F$ of the utilization at reset.
2. An 80% **credible interval** $[F^-, F^+]$ for $F$. (Throughout this doc
   "CI" is shorthand for *credible*, not *confidence* — the underlying
   posterior is Bayesian, see §3. We continue to use the looser "confidence"
   wording in user-facing strings only.)
3. For each threshold $C_{\text{thr}}$ (e.g. 100%), either a median ETA with
   an 80% CI, or `nil` if the threshold is unlikely to be reached before
   reset.
4. A confidence tag in $\{\text{Low}, \text{Medium}, \text{High}\}$.

This procedure is an *empirical Bayes* forecast: the prior on the rate $r$
and the path-noise variance $\sigma^2$ are both estimated from past data
(§3, §5) and then plugged into the posterior update, rather than being
themselves marginalized under a hyperprior. The conjugate update on $r$ and
the Monte Carlo over rate-and-path uncertainty are genuinely Bayesian; the
"empirical" qualifier refers to the prior, not the inference step.

A separate forecast is produced per gauge. The only cross-window sharing is
through the calibration constants (§5).

## The model in one paragraph

Inside the current window, utilization $u(t)$ grows at an unknown constant
rate $r$ plus Brownian noise (random behavioral pivots). We estimate $r$ from
the recent slope of the snapshots (OLS) and from a Gaussian prior fit on past
sessions, combined by conjugate update. The forecast at reset is the sum of
two random pieces: deterministic-time-times-uncertain-rate, plus Brownian
accumulation over the remaining horizon. Both pieces are Gaussian, so the
forecast is Gaussian and the CI is a $z$-quantile. The ETA has no closed
form, so we Monte Carlo it.

## Generative model and assumptions

For $s \in [0, \Delta t_{\text{rem}}]$ with $\Delta t_{\text{rem}} =
T_{\text{reset}} - t_{\text{now}}$,

$$u(t_{\text{now}} + s) = u_{\text{now}} + r s + W(s),
\qquad W(s) \sim \mathcal{N}(0, \sigma_{\text{session}}^2\, s),$$

with $r$ unknown and $W$ a Brownian motion independent of $r$. Three
assumptions:

1. **Linear conditional mean.** $r$ is constant within a window. Violated by
   abrupt mid-window mode shifts; partially absorbed by the recency window in
   §3.
2. **Brownian path noise.** Variance grows linearly with time. Approximately
   correct if behavioral pivots are mean-zero and not too heavy-tailed.
3. **Exchangeable past sessions.** Historical sessions are iid draws from the
   same noise process. Required for the calibration in §5.

## Notation

| Symbol | Meaning |
|---|---|
| $u(t), u_{\text{now}}$ | utilization at time $t$ and at now, $\in [0,1]$ |
| $t_{\text{now}}, T_{\text{reset}}, \Delta t_{\text{rem}}$ | now, reset, remaining horizon (h) |
| $r$ | true mean growth rate within window (h$^{-1}$) |
| $\hat{r}_{\text{OLS}}, \text{SE}_{\text{OLS}}$ | OLS estimate and its standard error |
| $\mu_0, \tau_0^2$ | Gaussian prior on $r$ from history |
| $\hat{r}_{\text{post}}, \tau_{\text{post}}^2$ | posterior on $r$ after the OLS update |
| $\sigma_{\text{session}}^2$ | Brownian diffusion coefficient (h$^{-1}$) |
| $F, \sigma_F^2$ | point forecast and its variance |
| $z_{0.9}$ | $\Phi^{-1}(0.9) \approx 1.2816$ |
| $K, \Delta s$ | MC trajectories, step size (defaults: 500, 5 min) |

## 3. Estimating the rate

Combine two sources of information about $r$.

**From the current window.** Let $\mathcal{R}$ be the snapshots in
$[t_{\text{now}} - \tau_{\text{recent}}, t_{\text{now}}]$ and $n = |\mathcal{R}|$.
For $n \geq 3$, fit $u_i = \alpha + r t_i + \epsilon_i$ by OLS:

$$\hat{r}_{\text{OLS}} = \frac{S_{tu}}{S_{tt}},
\qquad \text{SE}_{\text{OLS}}^2 = \frac{\hat{\sigma}_\epsilon^2}{S_{tt}},$$

with $S_{tt} = \sum (t_i - \bar t)^2$, $S_{tu} = \sum (t_i - \bar t)(u_i - \bar u)$,
and $\hat{\sigma}_\epsilon^2 = \frac{1}{n-2} \sum (u_i - \hat\alpha - \hat{r}_{\text{OLS}} t_i)^2$.
Default $\tau_{\text{recent}} = 30$ min.

Strictly, Brownian residuals are heteroskedastic and serially correlated, so
the iid-OLS variance formula above is an approximation. For the short recency
window the discrepancy is small and the point estimate is unbiased either way;
we accept the approximation and treat $\text{SE}_{\text{OLS}}^2$ as the
likelihood precision in the conjugate update below.

**From history.** For each past completed session $s$ with final value $u_s^*$
and duration $D_s$, define $\rho_s = u_s^* / D_s$. The prior mean is the
sample mean of $\rho_s$. For the prior variance, note that under the
generative model

$$\rho_s = r_s + \frac{W_s(D_s)}{D_s}, \qquad
\mathrm{Var}[\rho_s] = \mathrm{Var}[r_s] + \frac{\sigma_{\text{session}}^2}{D_s},$$

so the raw sample variance of $\rho_s$ overstates $\mathrm{Var}[r_s]$ by the
average path-noise contribution. Subtract it off:

$$\mu_0 = \mathrm{mean}_s \rho_s,
\qquad \tau_0^2 = \max\!\left(\mathrm{var}_s \rho_s
- \sigma_{\text{session}}^2 \cdot \mathrm{mean}_s\!\bigl(1/D_s\bigr),\ \epsilon\right),$$

with $\epsilon = 10^{-6}$ guarding against negative estimates from small
samples. This uses the most recent $\sigma_{\text{session}}^2$ from §5; the
two fits depend on each other but only loosely, and the daily refit cycle
converges quickly. On the very first fit (before §5 has run),
$\sigma_{\text{session}}^2 = 0$ and the correction is a no-op.

Fit at startup, refresh daily.

**Combine.** Normal-normal conjugacy gives the posterior $r \mid
\hat{r}_{\text{OLS}} \sim \mathcal{N}(\hat{r}_{\text{post}}, \tau_{\text{post}}^2)$:

$$\frac{1}{\tau_{\text{post}}^2} = \frac{1}{\tau_0^2} + \frac{1}{\text{SE}_{\text{OLS}}^2},
\qquad \hat{r}_{\text{post}} = \tau_{\text{post}}^2
\left(\frac{\mu_0}{\tau_0^2} + \frac{\hat{r}_{\text{OLS}}}{\text{SE}_{\text{OLS}}^2}\right).$$

Limits: prior dominates when $\text{SE}_{\text{OLS}}$ is large (early window);
data dominates when $\text{SE}_{\text{OLS}}$ is small (later in the window).
For $n < 3$ the OLS step is undefined; use $\hat{r}_{\text{post}} = \mu_0$,
$\tau_{\text{post}}^2 = \tau_0^2$.

## 4. Forecast distribution at reset

The point forecast is

$$F = u_{\text{now}} + \hat{r}_{\text{post}}\, \Delta t_{\text{rem}}.$$

By the law of total variance, conditioning on $r$:

$$\sigma_F^2 = \underbrace{\Delta t_{\text{rem}}^2\, \tau_{\text{post}}^2}_{\text{rate uncertainty}}
+ \underbrace{\Delta t_{\text{rem}}\, \sigma_{\text{session}}^2}_{\text{path noise}}.$$

Both contributions are Gaussian under the model, so
$u(T_{\text{reset}}) \sim \mathcal{N}(F, \sigma_F^2)$. The two terms have
different scaling: rate uncertainty is quadratic in horizon (dominates at
long horizons), path noise is linear (dominates at short).

**80% CI.** $[F - z_{0.9} \sigma_F,\, F + z_{0.9} \sigma_F]$, clipped to
$[0, 1]$ for display. The unclipped $F$ is retained for ETA computation.

## 5. Calibration of $\sigma_{\text{session}}^2$

**What we learn.** One scalar: how much utilization can drift from the trend
per hour of waiting. It is the only quantity history contributes to the
forecast *spread* (the prior $\mu_0, \tau_0^2$ contributes to the *mean*).

**Strategy.** Run the forecaster on past sessions, where we already know
the answer, and measure how wrong it was as a function of horizon. Read
$\sigma_{\text{session}}^2$ off the empirical error structure.

**Replay.** For each past session $s$ with reset value $u_s^*$ at $T_s$,
sample forecast points $t_f \in [t_s + \tau_{\text{recent}}, T_s - \Delta_{\min}]$
(default 6 per session, $\Delta_{\min} = 30$ min). At each $t_f$, run §3 on
snapshots in $[t_f - \tau_{\text{recent}}, t_f]$ to obtain
$\hat r_{\text{post}}(t_f)$, and the projection
$\hat F(t_f) = u(t_f) + \hat r_{\text{post}}(t_f) (T_s - t_f)$. Record

$$e_f = u_s^* - \hat F(t_f), \qquad \delta_f = T_s - t_f.$$

**Two sources of error in each residual.** The residual $e_f$ mixes (A) the
rate estimate being off at $t_f$, with the error compounded over $\delta_f$,
and (B) Brownian path noise accumulating over $\delta_f$. By the same
decomposition as §4,

$$\mathbb{E}[e_f^2 \mid \delta_f]
= \underbrace{\delta_f \, \sigma_{\text{session}}^2}_{\text{path noise (B)}}
+ \underbrace{\delta_f^2 \, \bar\tau^2}_{\text{rate uncertainty (A)}},$$

with $\bar\tau^2$ the average posterior rate variance at the forecast
points. The two contributions scale differently in $\delta_f$, which is what
lets us separate them. Naive averaging like $\sigma_{\text{session}}^2 \approx
\mathrm{mean}_f(e_f^2 / \delta_f)$ would not separate them and would bias the
estimate upward by a $\delta_f \bar\tau^2$ contamination.

**Joint regression.** Fit both unknowns by OLS of $e_f^2$ on
$[\delta_f, \delta_f^2]$ (no intercept):

$$(\hat a, \hat b) = \arg\min_{a, b} \sum_f
\bigl(e_f^2 - a \delta_f - b \delta_f^2\bigr)^2,
\qquad \sigma_{\text{session}}^2 := \max(\hat a, 10^{-6}).$$

The coefficient $\hat a$ on the linear term is the per-hour path-noise
variance: that is the quantity we want.

Note that this regression treats $\bar\tau^2$ as constant across forecast
points, while in reality $\tau_{\text{post}}^2(t_f)$ varies (it depends on how
many recent snapshots existed at $t_f$). If $\tau_{\text{post}}^2(t_f)$
correlates with $\delta_f$ - and it usually does, since $\delta_f$ is
anti-correlated with elapsed time in the session - the missing covariance
leaks into $\hat a$. A tighter alternative is to subtract the known
$\delta_f^2 \tau_{\text{post}}^2(t_f)$ piece as an offset and regress the
residual on $\delta_f$ alone; we keep the joint regression for simplicity and
absorb the bias into the daily refit.

**Why $\hat b$ is discarded.** Conceptually $\hat b$ estimates the same
quantity as $\tau_{\text{post}}^2$ in §3 (the rate-estimation variance),
just as a historical average instead of a fresh per-forecast value. The
per-forecast version in §3 uses today's actual snapshots, so it is strictly
more informative; $\hat b$ is redundant.

The $b \delta_f^2$ term still has to be *present in the regression*, though.
Without it, the $\delta_f^2 \bar\tau^2$ piece of $e_f^2$ has nowhere to go
but $\hat a$, biasing it upward. So $b$ is included as a nuisance parameter
(analogous to adjusting for a confounder in a regression: we control for its
contribution without caring about its fitted value), and discarded once
$\hat a$ is clean.

**Floor and refit.** The floor on $\hat a$ guards against negative
estimates from small samples (OLS does not enforce sign). Refit daily.

## 6. Threshold ETA

For threshold $C_{\text{thr}}$ with $u_{\text{now}} < C_{\text{thr}} \leq 1$,
the first-passage time is $T^* = \inf\{t > t_{\text{now}} : u(t) \geq
C_{\text{thr}}\}$, with $T^* = \infty$ if no crossing in
$[t_{\text{now}}, T_{\text{reset}}]$.

**Deterministic ETA (point estimate).** Setting $W \equiv 0$ and $r =
\hat{r}_{\text{post}}$:

$$\tilde{T}^* = t_{\text{now}} + \frac{C_{\text{thr}} - u_{\text{now}}}{\hat{r}_{\text{post}}},$$

defined when $\hat{r}_{\text{post}} > 0$ and $\tilde{T}^* \leq T_{\text{reset}}$;
else `nil`. Note that $\tilde{T}^*$ is **not** the median of $T^*$ under the
full stochastic model. Two effects pull in opposite directions:

- *BM skew (pulls median down).* For BM with fixed drift $\mu > 0$ hitting
  $a > 0$, $T^* \sim \mathrm{IG}(a/\mu, a^2/\sigma^2)$ with mean $a/\mu$ and
  right skew, so $\mathrm{median}(T^* \mid r{=}\mu) < a/\mu$.
- *Rate uncertainty (pulls median up).* Mixing over $r \sim \mathcal{N}
  (\hat r_{\text{post}}, \tau_{\text{post}}^2)$ inflates first-passage times
  asymmetrically: the map $r \mapsto a/r$ is convex on $r > 0$ and steepens
  sharply as $r \to 0^+$, so the right tail of $T^*$ stretches far more than
  the left tail compresses. Any mass at $r \leq 0$ contributes additional
  atoms at $T^* = \infty$. Both effects pull the median of $T^*$ above
  $\tilde T^*$.

Which effect wins depends on the regime, so $\mathrm{median}(T^*)$ can sit on
either side of $\tilde T^*$. The MC percentiles below are the authoritative
summary; $\tilde{T}^*$ is a cheap deterministic anchor, not an estimator of
the median.

**CI via Monte Carlo.** The first-passage time of a Brownian motion with
random Gaussian drift has no tractable closed form. Simulate $K = 500$
trajectories:

```
for k in 1..K:
    r_k    = N(r_hat_post, tau_post^2)     # may be negative; do NOT clip
    u_k[0] = u_now
    for j in 1..M, dt = step size of jth bucket:
        u_k[j] = u_k[j-1] + r_k * dt + N(0, sigma_session^2 * dt)
    T*_k = linear interpolation of first j with u_k[j] >= C_thr
         = infinity if none
```

Negative $r_k$ draws are kept as-is: those trajectories drift downward and
typically yield $T^*_k = \infty$, which is the correct outcome under the
model. Clipping to zero would bias crossings upward and contradict §4.

**Reported summaries.** Let $p_\infty$ be the fraction of trajectories with
$T^*_k = \infty$. Define $\tilde T^*_{\text{MC}} = \mathrm{median}(T^*_k)$
over the **full** sample (treating $\infty$ as larger than any finite
value):

- If $p_\infty \geq 0.5$: the true median is $\infty$. Report ETA as `nil`.
- If $p_\infty < 0.5$ but $p_\infty \geq 0.1$: the median is finite but the
  90th percentile is $\infty$. Report median, lower bound = 10th percentile
  of finite $T^*_k$, upper bound = `nil` (open-ended).
- If $p_\infty < 0.1$: report median and full 80% CI from the 10th and 90th
  percentiles of finite $T^*_k$.

RNG seed derived from $(t_{\text{now}}, T_{\text{reset}}, u_{\text{now}},
\hat{r}_{\text{post}}, \tau_{\text{post}}^2, \sigma_{\text{session}}^2)$ for
test reproducibility.

## 7. Confidence tag

Summarises the effective amount of evidence:

$$N_{\text{eff}} = \min\!\left(n,\; \frac{\tau_0^2}{\text{SE}_{\text{OLS}}^2} + |\mathcal{S}|\right).$$

| Tag | Threshold |
|---|---|
| High | $N_{\text{eff}} \geq 50$ |
| Medium | $15 \leq N_{\text{eff}} < 50$ |
| Low | $N_{\text{eff}} < 15$ |

Thresholds are heuristics; revisit after seeing real values.

## 8. Edge cases

| Case | Handling |
|---|---|
| $n < 3$ snapshots in $\mathcal{R}$ | Use prior alone: $\hat{r}_{\text{post}} = \mu_0$, $\tau_{\text{post}}^2 = \tau_0^2$ |
| Prior empty ($|\mathcal{S}| < 2$) | Suppress forecast; UI shows "collecting data" |
| Reset detected in $\mathcal{R}$ ($u_{i+1} < u_i$) | Drop pre-reset snapshots; refit |
| $C_{\text{thr}} \leq u_{\text{now}}$ | Threshold already crossed; return $T^* = t_{\text{now}}$ |
| $\hat{r}_{\text{post}} \leq 0$ | Deterministic ETA undefined; rely on MC summary |
| $F \notin [0, 1]$ | Clip for display; retain unclipped for downstream |

## 9. Worked example

5-hour session window. $t_{\text{start}} = $ 11:00, $T_{\text{reset}} = $ 16:00,
$t_{\text{now}} = $ 13:00, $u_{\text{now}} = 0.30$, so
$\Delta t_{\text{rem}} = 3$ h.

Last four snapshots ($\tau_{\text{recent}} = 30$ min):

| $i$ | $t_i$ (h from 12:30) | $u_i$ |
|---|---|---|
| 1 | 0.000 | 0.270 |
| 2 | 0.167 | 0.280 |
| 3 | 0.333 | 0.295 |
| 4 | 0.500 | 0.300 |

OLS: $\hat{r}_{\text{OLS}} \approx 0.0630$ h$^{-1}$,
$\text{SE}_{\text{OLS}}^2 \approx 6.30 \times 10^{-5}$.

Prior (suppose, from history, after the §3 noise correction):
$\mu_0 = 0.080$ h$^{-1}$, $\tau_0^2 = 3.6 \times 10^{-3}$.

Posterior: $\tau_{\text{post}}^2 \approx 6.19 \times 10^{-5}$,
$\hat{r}_{\text{post}} \approx 0.0633$ h$^{-1}$ (data dominates).

Calibration: $\sigma_{\text{session}}^2 = 2.5 \times 10^{-3}$ h$^{-1}$.

Forecast:

$$F = 0.30 + 0.0633 \cdot 3 \approx 0.490,$$
$$\sigma_F^2 \approx 9 \cdot 6.19\!\times\!10^{-5} + 3 \cdot 2.5\!\times\!10^{-3}
\approx 5.6\!\times\!10^{-4} + 7.5\!\times\!10^{-3} \approx 8.1\!\times\!10^{-3},$$
$$\sigma_F \approx 0.090.$$

80% CI: $[0.490 - 1.282 \cdot 0.090,\; 0.490 + 1.282 \cdot 0.090] \approx
[0.375,\; 0.605]$. Display: *"Projected 49% by reset (80% CI: 38%-60%)"*.

Path noise dominates rate uncertainty $13\times$. That is: we know the current
slope well; the spread is "you might pivot."

ETA to 100%: gap is 0.70, deterministic crossing at $13{:}00 + 0.70/0.0633
\approx 24{:}04$, far beyond reset. Returned as `nil`. The MC confirms by
producing infinite crossings for almost all trajectories.

## 10. Limitations and upgrade paths

- **Heavy-tailed pivots.** Real per-hour increments are heavier-tailed than
  Gaussian. The 80% CI undercovers in the tails. Cheap fix when calibration
  data is plentiful: replace $z_{0.9}$ with an empirical quantile of
  standardized residuals.
- **Constant-rate assumption.** A mode shift within the window (deep work
  $\to$ meetings) breaks (A1). The recency window $\tau_{\text{recent}}$
  absorbs slow drift but not abrupt shifts. The principled upgrade is a
  Kalman filter where $r$ is a slowly-varying latent process.
- **Global $\sigma_{\text{session}}^2$.** Pivot variance may depend on
  day-of-week or project type. Stratified calibration is the upgrade, but
  requires more sessions per stratum.
