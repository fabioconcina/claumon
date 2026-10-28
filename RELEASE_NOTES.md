## Hardening against DNS rebinding

- **The dashboard now only answers requests addressed to your machine.**
  DNS rebinding is a known technique against apps that serve a local web
  page: a specially crafted website points its own domain at 127.0.0.1 so
  the browser treats it as the local app. While such a page was open in a
  browser without local-network protection, it could read what the
  dashboard shows (sessions, memory files) and trigger its actions, such as
  interrupting a session. claumon now accepts only requests addressed to
  `localhost` or a loopback IP, which closes this off. Recent Chrome
  versions likely blocked it already. Updating is recommended.

- **If you reach claumon through a reverse proxy:** proxies that forward the
  browser's original `Host` header now get a 403 that tells you what to do.
  Add the hostname to the new `allowed_hosts` list in
  `~/.claumon/config.json`, e.g. `"allowed_hosts": ["claumon.example.com"]`.
  SSH tunnels, proxies that rewrite the Host to 127.0.0.1, and the VS Code
  extension need no changes.

- **Built with the newest Go 1.26 patch.** v0.21.3 was built with Go
  1.26.8, which lacks the fix for GO-2026-6809 in certificate verification
  (`crypto/x509`), used by claumon's HTTPS connections to Anthropic and
  GitHub. Release builds now always use the latest 1.26 patch.

- **`claumon update` refuses unverified downloads.** If a release had no
  checksums file, the self-updater installed the binary without checking
  it. It now stops instead.

- **GitHub link in the dashboard.** The top bar links to the project on
  GitHub, for release notes and issues. It's hidden in the VS Code panel,
  where links don't open.

- **Building from source no longer offers a downgrade.** A local build newer
  than the latest release showed that release as "available", and
  `claumon update` would have installed it.

- **Reporting security issues:** there is now a private channel, see
  [SECURITY.md](https://github.com/fabioconcina/claumon/blob/main/SECURITY.md).
