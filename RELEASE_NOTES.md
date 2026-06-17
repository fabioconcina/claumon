## VS Code extension

- **New: a companion VS Code extension.** Your live session/weekly usage now
  shows in the editor status bar, with the forecast projection inline
  (e.g. `45%->72% session`) and a hover tooltip that breaks down session/weekly
  usage, reset times, and the forecast (projected % with its 80% CI and ETA).
  `Claumon: Open Dashboard` embeds the full dashboard in an editor panel. It's a
  thin client over the same `/api/usage` + SSE feed, connects to a claumon
  server you're already running (default `localhost:3131`), and ships with zero
  runtime dependencies. Install it with a single command (it fetches the latest
  `.vsix` and runs `code --install-extension`) - see the
  [README](https://github.com/fabioconcina/claumon#vs-code-extension).

- **Docs refresh.** Tightened the README feature list, streamlined the landing
  page and its install steps, and documented the macOS Gatekeeper unblock step
  alongside the existing Windows one.

No server, forecast-model, pricing, or storage changes - the binary is unchanged
from the previous release; this release adds the VS Code extension and docs.
