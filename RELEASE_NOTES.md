## Forecast model v2.1, update badge, Fable 5 pricing

- **Forecast model v2.1: the 80% CI can no longer invert.** The reported
  interval is now the raw Monte Carlo terminal p10/p90, uncapped. v2.0 capped
  only the upper edge at 100%, so when projected demand blew past the limit
  the popup could show nonsense like "80% CI 134%-100%" (lower above upper).
  The forecast measures demand, which legitimately exceeds the window limit;
  overshoot magnitude is signal and the gauge ring still saturates at 100%.
  Reporting-convention change only: the generative model, calibration, MC,
  and ETA are untouched. Spec updated (MODEL.tex/pdf), v2.0 archived under
  `internal/forecast/archive/v2.0/`, regression test added.

- **The dashboard tells you when a newer claumon is out.** A background check
  polls GitHub for the latest release shortly after startup and then daily,
  reusing the existing updater plumbing. When the running build is behind, an
  accent-colored pill appears next to the version in the top bar, linking to
  the releases page; live pages light up via an `update_available` SSE event
  without a reload. `/api/info` now reports `update_available`,
  `latest_version`, and `releases_url`. Best-effort and silent on failure;
  dev builds never trigger it.

- **Claude Fable 5 pricing.** Cost estimates now cover `claude-fable-5`
  ($10/$50 per MTok input/output, cache prices following the usual convention:
  0.1x read, 1.25x 5-minute write, 2x 1-hour write). Synced to the embedded
  fallback; pricing date bumped to 2026-06-10.

- **Bigger forecast popups.** The session and weekly trajectory modals grew
  from 640px to 880px wide (still capped at 94vw on small screens); the chart
  SVG scales with them, so the simulated paths and axis labels are easier to
  read.

- **README and landing page repositioned.** The pitch now leads with the gap
  claumon fills: Anthropic's usage analytics dashboard is for Team/Enterprise
  org admins, not individual Pro/Max plans. New "How it compares" table vs
  ccusage, Claude-Code-Usage-Monitor, and claude-usage, and the forecast
  model spec (MODEL.pdf) is surfaced on both pages. No functional changes.

No forecast-model or storage changes.
