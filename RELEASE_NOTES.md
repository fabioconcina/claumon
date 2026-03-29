## Fixed

- **Sessions with large JSONL lines no longer silently disappear** — increased scanner buffer from 2MB to 10MB and made scanner errors non-fatal, so sessions with oversized tool-result lines are still parsed from the data before the error
