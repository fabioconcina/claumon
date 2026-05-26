## Fixes

- **Forecast lower bound no longer dips below current utilization.** The 80% CI was being clipped to `[0%, 100%]` for display, but utilization within a reset window is monotone non-decreasing, so the Gaussian left tail going below `u_now` produced unphysical readouts like "current 6%, 80% CI: 0%-16%". The displayed bounds are now floored at `u_now`. The unclipped point forecast `F` is preserved for downstream ETA computation, and the model spec in [`internal/forecast/MODEL.pdf`](internal/forecast/MODEL.pdf) is updated to match.
