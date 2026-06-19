# Claumon for VS Code

A thin VS Code companion for [claumon](https://github.com/fabioconcina/claumon).
It surfaces your live Claude Code usage in the status bar and embeds the full
claumon dashboard in an editor panel, so you do not have to keep a browser tab
open.

This extension is a **client only**: it connects to a claumon server you are
already running (default `localhost:3131`). It does not start or bundle claumon.

## Install

One command grabs the latest `.vsix` from GitHub and installs it (needs `code`
on your PATH):

**macOS / Linux**

```bash
curl -fsSL "$(curl -fsSL https://api.github.com/repos/fabioconcina/claumon/releases | grep -o 'https://github.com/[^"]*\.vsix' | head -1)" -o /tmp/claumon.vsix && code --install-extension /tmp/claumon.vsix
```

**Windows (PowerShell)**

```powershell
$u = (irm https://api.github.com/repos/fabioconcina/claumon/releases | %{ $_.assets } | ?{ $_.name -like '*.vsix' } | select -First 1).browser_download_url
iwr $u -OutFile $env:TEMP\claumon.vsix; code --install-extension $env:TEMP\claumon.vsix
```

Then reload VS Code. To update later, re-run the same command.

Prefer to do it by hand? Download `claumon-<version>.vsix` from an `ext-v*`
[release](https://github.com/fabioconcina/claumon/releases) and run **Extensions:
Install from VSIX...** from the Command Palette.

## Features

- **Status bar item** showing your current session (or weekly) usage percentage,
  updated live over claumon's SSE stream. When a forecast is available it also
  shows the projected percentage at reset (e.g. `45%->72% session`). The tooltip
  breaks down session/weekly (and Opus/Sonnet) percentages, reset times, the
  forecast (projected % with its 80% confidence interval and threshold ETA), and
  any auth or poll errors. It turns yellow at >= 75% and red at >= 90% of
  current usage, and also turns yellow when the forecast projects you'll exceed
  the limit before the window resets - showing a flame icon while you're still
  on track to hit it.
- **Dashboard panel** (`Claumon: Open Dashboard`) embedding the live claumon web
  UI. If the server is not reachable, it shows an offline message with a Retry
  button.
- Resilient connection: auto-reconnect with backoff, graceful "offline" state,
  and live response to settings changes.

## Requirements

- A running claumon server (see the parent project). By default the extension
  looks for it at `http://localhost:3131`.
- VS Code 1.85 or newer.

## Settings

| Setting | Default | Description |
| --- | --- | --- |
| `claumon.host` | `localhost` | Hostname of the claumon server. |
| `claumon.port` | `3131` | Port of the claumon server. |
| `claumon.statusBar.metric` | `session` | Which metric to show in the status bar (`session` or `weekly`). |

## Commands

- **Claumon: Open Dashboard** : open the embedded dashboard panel.
- **Claumon: Reconnect** : force a reconnect to the SSE stream and refresh.

## Development

```bash
npm ci               # clean, locked install (no install scripts; see .npmrc)
npm run compile      # tsc -> dist/*.js
npm run watch        # rebuild on change
npm run typecheck    # tsc --noEmit
```

Press <kbd>F5</kbd> in this folder to launch the Extension Development Host.
Start claumon first so there is something to connect to.

## Notes

This lives as a subfolder of the claumon repo so server and client changes stay
atomic, but it has an isolated Node toolchain and its own version. It is compiled
with `tsc` and has **zero runtime dependencies** (Node 18+ `fetch` powers the SSE
client), so an install pulls only the TypeScript toolchain and type stubs.
