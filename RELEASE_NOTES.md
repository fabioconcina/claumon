## New

- **Memory consolidation** — detects duplicate memories across projects using similarity scoring (shared entities, text bigrams, frontmatter type). Shows actionable suggestions with a "Copy prompt" button to paste into Claude Code for merging.
- **Version display** — app version shown in the topbar, set via git tags at build time
- **GFM markdown rendering** — memory files now render with GitHub Flavored Markdown (tables, strikethrough) and hard line breaks

## Install

Single binary, zero config. Download, run, open `http://localhost:3131`.

Reads credentials from `~/.claude/.credentials.json` or your OS credential store. If missing, session tracking still works — only API usage gauges are unavailable.
