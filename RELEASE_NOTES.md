## History window selector; pricing for Opus 5.5 and Mythos

- **Choose how far back the daily charts go.** A new dropdown next to the
  "Billable only" toggle switches both the Daily Tokens and Equiv. API Cost
  charts between 7, 14, 30, 60 and 90 days. The default stays at 14 days
  and the choice is remembered across reloads. Longer windows use tighter
  bars, hide per-bar values, and thin the date axis so 90 days stay
  readable.

- **Three models added to the pricing table.** Claude Opus 5.5 ($4/$20 per
  MTok, cache reads at $0.20), Claude Mythos 5.1 (same rates as Fable 5.1)
  and Claude Mythos 5 (same rates as Fable 5). Opus 5.5 sessions were
  previously costed at Opus 5 rates through the prefix match, so they were
  overcounted by about 20%.

- **Mythos IDs fall back to Mythos 5.1.** Unrecognised mythos model IDs now
  resolve to Mythos 5.1 pricing instead of Sonnet 5.
