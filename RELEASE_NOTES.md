## Fixed

- **Windows service install no longer requires admin** — replaced `schtasks` (which needed elevation) with a VBScript in the Startup folder. No admin privileges required, launches hidden with no console window.
