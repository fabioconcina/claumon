## Features

- **Forecast trajectory modal.** Click "Projected X% at reset" on a session or weekly gauge to open a modal showing the actual Monte Carlo trajectories the §6 simulator is producing: a fog of ~120 sampled paths, the empirical 10/90 percentile band traced from the same paths, the posterior mean line, observed snapshots so far, and a small first-passage histogram on the threshold line with the MC median ETA. New endpoint `GET /api/forecast/sample?gauge=session|weekly` re-runs the MC with trajectories collected; payload is subsampled in both the trajectory and time dimensions so weekly stays under ~200KB. The §6 MC core is shared with `EstimateETA` so the modal and the cached ETA summary always agree.

- **Versioned forecast model.** The forecast spec now carries a `forecast.ModelVersion` identifier (`v1.0` today). It's surfaced on every `/api/forecast` and `/api/forecast/sample` response, in the modal subtitle, and in the diagnostics report. Retired specs will move to `internal/forecast/archive/<version>/` when the model changes meaningfully; [`internal/forecast/CHANGELOG.md`](internal/forecast/CHANGELOG.md) summarises each bump.

- **`claumon diagnostics` subcommand.** Replays the forecaster across past completed sessions and prints calibration metrics (80% CI coverage, MAE of `F`, ETA accuracy) stamped with the model version. Useful for deciding whether a model change actually improves accuracy before shipping it.

## Internals

- CI workflows bumped to Node-24-compatible action versions (`actions/checkout` v4→v6, `actions/setup-go` v5→v6, `goreleaser/goreleaser-action` v6→v7); workflow Go version aligned to `go.mod` (1.26).
