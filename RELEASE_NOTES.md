## Fixed

- **Windows service restart port race** — after killing the old process, wait for the TCP port to become available before launching the new instance. Prevents intermittent "address already in use" failures during `claumon service restart`.
