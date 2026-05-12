## Performance

- **Stop re-parsing every JSONL on every session change.** The watcher callback called `DiscoverDailyAggregates`, which re-read and re-parsed every session file under `~/.claude/projects` on each event. With many concurrent live sessions, this burned hundreds of MB/s of I/O and pegged a CPU core. Parsed per-day buckets are now memoized in-process and invalidated by `(path, mtime, size)`, so only changed files are re-parsed.
