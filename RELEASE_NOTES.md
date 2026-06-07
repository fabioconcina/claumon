## Forecast: the projected percentage is easier to read at a glance

- **The projected-at-reset percentage is now bold and color-coded by risk.** On
  the Session and Weekly gauge cards the forecast line kept the same wording, but
  the projected number itself is now bold and tinted green / yellow / red using
  the same thresholds as the gauge ring and favicon (`gaugeColor`: green below
  50%, yellow 50-80%, red 80% and up). A high projection now reads as red without
  having to parse the small print.

- **The trajectory popup matches.** The popup footer applies the same color-coding
  to its projected percentage, and the 80% CI moved into parentheses
  (`Projected at reset: 87% (80% CI 78%-95%)`) to read as a single phrase.

- **The terminal point at reset is highlighted on the plot.** The forecast plot
  now draws a bold, color-coded dot where the posterior-mean line lands at reset.
  That endpoint is the model's main estimate, so it now stands out from the
  trajectory fog instead of being the unmarked end of a thin line.

All three changes live in the embedded frontend ([`web/index.html`](web/index.html))
and reuse the existing `gaugeColor` helper. No backend, pricing, or forecast-model
changes.
