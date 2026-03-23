## Fixed

- **Windows credential lookup** — added `-NonInteractive` and `-ExecutionPolicy Bypass` flags to PowerShell credential reader, with better error messages surfacing stderr
- **Windows setup instructions** — added Defender unblock steps to README and landing page
- **Gitignore** — exclude `claumon.exe` binary

## Install

Single binary, zero config. Download, run, open `http://localhost:3131`.

Reads credentials from `~/.claude/.credentials.json` or your OS credential store. If missing, session tracking still works — only API usage gauges are unavailable.
