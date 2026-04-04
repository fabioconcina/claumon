## Added

- **Background service** — `claumon service install` registers claumon to start automatically on login. Works cross-platform: LaunchAgent on macOS, systemd user unit on Linux, scheduled task on Windows. No root required.
- **Self-update** — `claumon update` checks GitHub releases, downloads the correct binary for your platform, replaces the current one, and restarts the service if installed.
- **Version subcommand** — `claumon version` prints the version and exits.

## Platform handling

- **macOS** — automatically clears quarantine/provenance attributes and re-signs the binary after download or service restart, preventing Gatekeeper from blocking execution.
- **Windows** — strips Mark of the Web (Zone.Identifier) on downloaded binaries to avoid SmartScreen blocks.
- **Linux** — README documents `loginctl enable-linger` for keeping the service running after logout.
