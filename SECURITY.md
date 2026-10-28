# Security Policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub:
[Report a vulnerability](https://github.com/fabioconcina/claumon/security/advisories/new).
Don't open a public issue for anything exploitable.

I'll acknowledge reports within a few days and keep you posted until a fix
ships. Credit in the release notes is yours if you want it.

## Supported versions

Only the latest release of the `claumon` binary and the latest version of the
VS Code extension receive fixes. Update with `claumon update` or
`brew upgrade claumon`.

## Scope

claumon reads your Claude Code credentials, session transcripts, and memory
files, and serves them on a local dashboard. Issues that are in scope include:

- anything that lets a web page, another user, or a remote host read that data
  or trigger the dashboard's actions (killing sessions, trashing memories)
- the credential token reaching any host other than `api.anthropic.com`
- tampering with the self-updater or release artifacts
- the VS Code extension being redirected or abused by an untrusted workspace

Out of scope: attacks that already require code execution as your user, and
exposing the dashboard to a network yourself without a reverse proxy that
handles authentication.
