## Forecast model → v2.0

- **Utilization is now modeled as a monotone, positive-only process.** v1.x
  modeled within-window growth as Brownian motion, which can drift downward: the
  Monte Carlo fan-chart visibly dipped, and the lower confidence bound had to be
  clipped back up to current utilization because the symmetric Gaussian tail
  fell below it (tokens never un-spend). v2.0 replaces the path law with a Gamma
  process (non-decreasing by construction) matched to the same mean and
  variance, so: simulated trajectories never decrease; the 80% interval is read
  off the Monte Carlo terminal quantiles, which are right-skewed with a lower
  bound that rests at current utilization on its own (no clip); and the
  threshold ETA no longer counts paths that dipped below and re-crossed later.
  The point forecast itself is unchanged. The v1.2 spec is preserved under
  [`internal/forecast/archive/v1.2/`](internal/forecast/archive/v1.2/); the math
  is in [`internal/forecast/MODEL.pdf`](internal/forecast/MODEL.pdf) and
  [`internal/forecast/CHANGELOG.md`](internal/forecast/CHANGELOG.md).

## New: `--port` and `--db` flags

- **Run a second instance without editing your config.** `--port` overrides the
  dashboard port and `--db` the database path, both from the command line (e.g.
  `claumon --port 3132 --db /tmp/test.db`). Handy for trying a build on another
  port against a copy of your data while your main instance keeps running.

## Internals

- **The forecast benchmark now scores the shipped distribution.** The
  `benchtools` bench harness scored a Gaussian rebuilt from the forecast's mean
  and spread; with v2.0's skewed, floored interval that proxy no longer matched
  what ships. `Predictive` now carries the Monte Carlo terminal sample and is
  scored with an unbiased sample CRPS and empirical quantiles, so CRPS,
  coverage, and pinball reflect the actual v2.0 distribution. Development and
  validation tool only; it does not affect the dashboard.
