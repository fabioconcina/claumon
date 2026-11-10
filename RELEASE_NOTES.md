## More accurate costs

- **1-hour cache writes are priced at the 1-hour rate.** Claude Code writes
  its prompt cache with the 1-hour lifetime, which costs 2x base input
  rather than the 1.25x of the default 5 minutes. claumon priced every
  cache write at the 5-minute rate, so costs were undercounted. Each
  response's split between the two is now read from the transcript. On my
  last 30 days of Opus 5.5 use, the corrected total is 16% higher. Past
  days are recomputed at startup, as long as their transcripts still exist.

- **Fable 5.2, Mythos 5.2, Sonnet 5.5 and Haiku 5.5 pricing.** Fable 5.2
  and Mythos 5.2 are $8/$40 per MTok with cache reads at $0.20, Sonnet 5.5
  is $2/$10 with cache reads at $0.10, and Haiku 5.5 is $0.10/$0.50.
  Haiku 5.5 sessions were previously costed at Haiku 4.5 rates, 10x too
  high. Haiku 5.5's higher rate for prompts over 100K tokens isn't modeled
  yet, so long Haiku 5.5 prompts are undercounted.

- **Family fallbacks track the current generation.** Unknown fable, mythos,
  opus, sonnet and haiku IDs now resolve to Fable 5.2, Mythos 5.2, Opus 5.5,
  Sonnet 5.5 and Haiku 5.5.

## Fewer stuck connections

- **A hidden dashboard lets go of its live connection.** Browsers allow six
  connections per address, shared by every tab and VS Code panel. A
  dashboard left open in a background tab now disconnects after 15 seconds
  out of sight and reconnects (and refreshes) when you come back, so
  several open dashboards no longer leave one waiting for updates. In VS
  Code this needs the next release of the claumon extension.

- **Shorter README.** Service setup per OS, release verification, live
  updates, data storage and the architecture moved to
  [REFERENCE.md](https://github.com/fabioconcina/claumon/blob/main/REFERENCE.md).
