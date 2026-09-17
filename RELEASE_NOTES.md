## Pricing: Fable 5.1, Opus 5, Sonnet 5

- **Three current models added to the pricing table.** Claude Fable 5.1
  ($10/$50 per MTok, cache reads at $0.25), Claude Opus 5 ($5/$25) and
  Claude Sonnet 5 ($2/$10). Opus 5 and Sonnet 5 sessions were previously
  unrecognised and costed at Sonnet 4.6 rates, so Opus 5 sessions were
  undercounted and Sonnet 5 sessions overcounted.

- **Fable 5.1 cache reads no longer overpriced.** The model ID used to
  prefix-match the Fable 5 row, which bills cache reads at $1.00 instead of
  Fable 5.1's $0.25. Since long Claude Code sessions are dominated by cache
  reads, Fable 5.1 session costs were inflated by roughly 40%. Model
  matching now prefers the longest matching pricing key.

- **Family fallbacks track the current generation.** Unknown fable, opus and
  sonnet IDs now resolve to Fable 5.1, Opus 5 and Sonnet 5 respectively.
