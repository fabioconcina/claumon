import * as vscode from "vscode";
import {
  UsagePayload,
  AuthStatusPayload,
  ForecastPayload,
  SessionSummary,
  getConfig,
} from "./client";

/** Local time-of-day for an RFC3339 timestamp, e.g. "14:05" (""  if invalid). */
function fmtClock(iso?: string): string {
  if (!iso) {
    return "";
  }
  const d = new Date(iso);
  if (isNaN(d.getTime())) {
    return "";
  }
  return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

/** Friendly model name, mirroring the dashboard's shortModel. */
function shortModel(m?: string): string {
  if (!m) {
    return "";
  }
  return m.replace("claude-", "").replace(/-\d{8}$/, "");
}

/** Epoch ms for an RFC3339 timestamp, or 0 if absent/invalid. */
function ts(iso?: string): number {
  if (!iso) {
    return 0;
  }
  const t = new Date(iso).getTime();
  return isNaN(t) ? 0 : t;
}

/**
 * The model of the session the user is most plausibly driving: prefer a running
 * session whose cwd is an open workspace folder, then any running session, then
 * the most recently active session overall. Ties broken by last activity.
 * Returns "" when no session (or no model) is known.
 */
function activeModel(sessions: SessionSummary[]): string {
  if (sessions.length === 0) {
    return "";
  }
  const roots = (vscode.workspace.workspaceFolders ?? []).map((f) => f.uri.fsPath);
  const inWorkspace = (s: SessionSummary): boolean =>
    !!s.cwd && roots.some((r) => s.cwd === r || s.cwd!.startsWith(r + "/"));
  const score = (s: SessionSummary): number =>
    (s.is_running && inWorkspace(s) ? 2 : s.is_running ? 1 : 0);

  let best: SessionSummary | undefined;
  for (const s of sessions) {
    if (
      !best ||
      score(s) > score(best) ||
      (score(s) === score(best) && ts(s.last_activity) > ts(best.last_activity))
    ) {
      best = s;
    }
  }
  return shortModel(best?.model);
}

/**
 * Owns the status bar item and renders the latest known state into it.
 * States, in priority order: offline (no connection) > pending (connected but
 * no poll yet) > auth problem > normal usage.
 */
export class StatusBar {
  private readonly item: vscode.StatusBarItem;
  private connected = false;
  private usage?: UsagePayload;
  private auth?: AuthStatusPayload;
  private sessions: SessionSummary[] = [];

  constructor() {
    this.item = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
    this.item.command = "claumon.openDashboard";
    this.item.show();
    this.render();
  }

  setConnected(connected: boolean): void {
    this.connected = connected;
    this.render();
  }

  setUsage(usage: UsagePayload): void {
    // Note: do NOT set connected here. The live-connection flag is owned by
    // onConnectionChange; a late one-shot poll must not repaint a dropped
    // connection as live.
    this.usage = usage;
    this.render();
  }

  setAuth(auth: AuthStatusPayload): void {
    this.auth = auth;
    this.render();
  }

  setSessions(sessions: SessionSummary[]): void {
    this.sessions = sessions ?? [];
    this.render();
  }

  /** Re-render after a config change (metric/host/port may have changed). */
  refresh(): void {
    this.render();
  }

  private render(): void {
    const { metric } = getConfig();

    if (!this.connected) {
      this.item.text = "$(plug) claumon: offline";
      this.item.tooltip = "Not connected to the claumon server. Click to open the dashboard.";
      this.item.backgroundColor = new vscode.ThemeColor("statusBarItem.warningBackground");
      return;
    }

    const u = this.usage;
    if (!u || u.available === false) {
      this.item.text = "$(sync~spin) claumon: …";
      this.item.tooltip = "Connected to claumon, waiting for the first usage poll.";
      this.item.backgroundColor = undefined;
      return;
    }

    const pct = metric === "weekly" ? u.weekly_pct : u.session_pct;
    const label = metric === "weekly" ? "weekly" : "session";
    const fc = metric === "weekly" ? u.forecast?.weekly : u.forecast?.session;
    const pctText = typeof pct === "number" ? `${Math.round(pct)}%` : "?";
    const projText =
      fc && typeof fc.projected_pct === "number" ? `→${Math.round(fc.projected_pct)}%` : "";

    // Two orthogonal signals. Background = severity (max of current usage and
    // forecast); icon = whether the trajectory is still working against us.
    // A red-but-pulse badge means "high now, but you've eased off"; red-and-
    // flame means "high and still climbing into the wall".
    const high = typeof pct === "number" && pct >= 90;
    const warn = typeof pct === "number" && pct >= 75;
    const forecastBad = typeof fc?.projected_pct === "number" && fc.projected_pct >= 100;

    const icon = forecastBad ? "$(flame)" : "$(pulse)";
    const model = activeModel(this.sessions);
    const modelText = model ? ` | ${model}` : "";
    this.item.text = `${icon} ${pctText}${projText} ${label}${modelText}`;
    this.item.backgroundColor = high
      ? new vscode.ThemeColor("statusBarItem.errorBackground")
      : warn || forecastBad
        ? new vscode.ThemeColor("statusBarItem.warningBackground")
        : undefined;

    this.item.tooltip = this.buildTooltip(u);
  }

  private buildTooltip(u: UsagePayload): vscode.MarkdownString {
    const md = new vscode.MarkdownString();
    md.appendMarkdown("**Claumon**\n\n");

    const line = (label: string, pct?: number | null, reset?: string): string => {
      if (typeof pct !== "number") {
        return "";
      }
      const resetText = reset ? ` (resets in ${reset})` : "";
      return `- ${label}: **${Math.round(pct)}%**${resetText}\n`;
    };

    md.appendMarkdown(line("Session", u.session_pct, u.session_reset));
    md.appendMarkdown(this.forecastLines(u.forecast?.session));
    md.appendMarkdown(line("Weekly", u.weekly_pct, u.weekly_reset));
    md.appendMarkdown(this.forecastLines(u.forecast?.weekly));
    if (typeof u.weekly_opus_pct === "number") {
      md.appendMarkdown(line("Weekly (Opus)", u.weekly_opus_pct));
    }

    if (u.poll_error) {
      md.appendMarkdown(`\n⚠️ Poll error: ${u.poll_error}\n`);
    }
    const authStatus = this.auth?.status;
    if (authStatus && authStatus !== "ok") {
      const msg = this.auth?.message ? `: ${this.auth.message}` : "";
      md.appendMarkdown(`\n⚠️ Auth ${authStatus}${msg}\n`);
    }

    md.appendMarkdown("\n_Click to open the dashboard._");
    return md;
  }

  /**
   * Forecast detail nested under a gauge line: projected % at reset with its
   * 80% CI, and the soonest threshold ETA. Empty when no forecast is available.
   */
  private forecastLines(fc?: ForecastPayload): string {
    if (!fc || typeof fc.projected_pct !== "number") {
      return "";
    }
    const proj = Math.round(fc.projected_pct);
    const lo = Math.round(fc.lower_80_pct);
    const hi = Math.round(fc.upper_80_pct);
    let out = `  - ↳ projected **${proj}%** at reset (80% CI ${lo}-${hi}%)\n`;
    const eta = (fc.etas ?? []).find((e) => e.median);
    if (eta) {
      const clock = fmtClock(eta.median);
      if (clock) {
        out += `  - ↳ ETA to ${Math.round(eta.threshold_pct)}%: ${clock}\n`;
      }
    }
    return out;
  }

  dispose(): void {
    this.item.dispose();
  }
}
