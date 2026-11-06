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
