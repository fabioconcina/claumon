## Dashboard

- **Live-updating forecast chart.** The forecast (trajectory) popup now
  refreshes in place while it stays open: each new usage snapshot re-samples and
  re-draws the chart, so the projected trajectories, confidence band, and ETA
  keep moving instead of freezing on the values captured when you opened it. No
  more closing and reopening to see the latest projection.

- **Observed line connects to "now".** Stored usage snapshots can lag the current
  time by a few minutes, which used to leave a gap between the end of the
  observed (teal) line and the "now" marker where the trajectories originate. The
  observed line now extends to the current point, so it joins the trajectory fan
  cleanly.

- **Billions formatting.** Large token totals now switch to a `B` suffix once
  they reach a billion (e.g. `1.5B`) instead of continuing to report thousands of
  millions (`1500.0M`).
