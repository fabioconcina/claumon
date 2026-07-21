## Dashboard

- **Weekly Fable gauge.** The dashboard now shows a dedicated gauge for the
  Fable-only weekly limit, with utilization percentage, color-coded band, and
  reset countdown next to the Session and Weekly gauges. Under the hood this
  is fully dynamic: the usage API reports per-model weekly limits through a
  new `limits` array (the legacy `seven_day_*` buckets are empty), and the
  dashboard renders one gauge per model the API reports, so gauges for other
  models appear automatically as their limits show up. `/api/usage` exposes
  the data as `weekly_scoped`.

- **Trash explorer.** The Memory tab has a new **Trash** view listing every
  recoverable deletion: content preview, original location, deletion time,
  days until permanent removal, and a one-click **restore**. No more digging
  through `~/.claude/.claumon-trash` to find where a deleted memory went.
