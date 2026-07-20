## Security & safety

- **Local-only by default.** The dashboard now binds explicitly to
  `127.0.0.1`, so session details, memory files, and process controls are not
  exposed to the local network. Cross-origin browser mutations are rejected,
  and API responses no longer opt into permissive CORS.

- **Recoverable memory deletion.** Deleting a memory file now moves it into
  claumon's local trash instead of removing it permanently. An **Undo** action
  restores both the file and its `MEMORY.md` index entry. Trash is cleaned up
  automatically after 30 days.

- **Verifiable releases.** Release builds now include per-binary software bills
  of materials and GitHub build-provenance attestations alongside the existing
  SHA-256 checksums.

*(v0.18.1 re-releases v0.18.0 with the build-provenance attestation step
actually working: the workflow pointed at a checksums filename goreleaser
didn't produce.)*
