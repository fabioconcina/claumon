## New

- **Running processes section** — dedicated table showing all live Claude Code processes with PID, chat title, project, entrypoint, uptime, and a Stop button to gracefully terminate (SIGINT)
- **Session process detection** — sessions table now shows green "active" badge for running sessions
- **Today / Recent toggle** — switch between today's sessions and the 50 most recent sessions across all time

## Changed

- **Optimized all-sessions loading** — uses file modification time to sort and cap results, avoiding parsing hundreds of JSONL files
- Process killing is centralized in the Running Processes section (not in session detail)
