## Fixed

- **Heatmap and today's tokens no longer include prior days** — sessions resumed across midnight credited their full lifetime tokens to today's aggregates and the hourly heatmap. Both are now bucketed by each message's own timestamp.
- **Session detail timestamps now show dates across day boundaries.**
- **Process liveness detection on Windows.**
- **Time-sensitive store tests** that assumed March 2026 dates.

## Changed

- **Expanded tool call detail in the session view.**
