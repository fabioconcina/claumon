import * as vscode from "vscode";

/** First-passage forecast for one threshold. Times are RFC3339 or absent. */
export interface ForecastEta {
  threshold_pct: number;
  median?: string;
  lower?: string;
  upper?: string;
  p_inf?: number;
}

/**
 * Per-gauge forecast snapshot, mirrored from internal/forecast.Payload. All
 * percentages are 0-100. Present only once enough history exists to fit.
 */
export interface ForecastPayload {
  projected_pct: number;
  lower_80_pct: number;
  upper_80_pct: number;
  horizon_hours?: number;
  etas?: ForecastEta[];
}

/** One per-model weekly limit from the server's `weekly_scoped` list. */
export interface ScopedWeeklyLimit {
  name: string;
  pct: number;
  reset?: string;
}

/**
 * Usage payload returned by GET /api/usage and broadcast on the `usage` SSE
 * event. Only the fields the extension renders are typed; the server may send
 * more. All percentage fields are 0-100.
 */
export interface UsagePayload {
  session_pct?: number;
  weekly_pct?: number;
  session_reset?: string;
  weekly_reset?: string;
  session_reset_at?: string;
  weekly_reset_at?: string;
  /** Legacy per-model field from servers before weekly_scoped existed. */
  weekly_opus_pct?: number | null;
  weekly_scoped?: ScopedWeeklyLimit[];
  last_poll_at?: number;
  poll_error?: string;
  available?: boolean;
  /** Per-gauge forecasts, keyed by gauge. Absent until a fit is available. */
  forecast?: { session?: ForecastPayload; weekly?: ForecastPayload };
}

/**
 * Subset of a parser.SessionSummary the extension renders. The server sends
 * many more fields; only those needed to pick and label the active session are
 * typed here. Times are RFC3339.
 */
export interface SessionSummary {
  id: string;
  model?: string;
  cwd?: string;
  last_activity?: string;
  is_running?: boolean;
}

/** Subset of GET /api/info the extension cares about. */
export interface InfoPayload {
  version?: string;
  subscription_type?: string;
  auth_status?: string;
  auth_message?: string;
  update_available?: boolean;
}

/** Auth status SSE payload / GET /api/auth/status. */
export interface AuthStatusPayload {
  status?: string;
  message?: string;
}

export function getConfig(): { host: string; port: number; metric: "session" | "weekly" } {
  const cfg = vscode.workspace.getConfiguration("claumon");
  return {
    host: cfg.get<string>("host", "127.0.0.1"),
    port: cfg.get<number>("port", 3131),
    metric: cfg.get<"session" | "weekly">("statusBar.metric", "session"),
  };
}

export function baseUrl(): string {
  const { host, port } = getConfig();
  // Bracket IPv6 literals (e.g. ::1) so the URL is well-formed.
  const h = host.includes(":") && !host.startsWith("[") ? `[${host}]` : host;
  return `http://${h}:${port}`;
}

/** One-shot JSON GET with a short timeout. Throws on network/HTTP error. */
async function getJSON<T>(path: string, timeoutMs = 4000): Promise<T> {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), timeoutMs);
  try {
    const res = await fetch(`${baseUrl()}${path}`, { signal: ctrl.signal });
    if (!res.ok) {
      throw new Error(`HTTP ${res.status}`);
    }
    return (await res.json()) as T;
  } finally {
    clearTimeout(timer);
  }
}

export function getUsage(): Promise<UsagePayload> {
  return getJSON<UsagePayload>("/api/usage");
}

export function getInfo(): Promise<InfoPayload> {
  return getJSON<InfoPayload>("/api/info");
}

/** Today's sessions (the default range). Used for the initial model paint. */
export function getSessions(): Promise<SessionSummary[]> {
  return getJSON<SessionSummary[]>("/api/sessions");
}

export type SseHandlers = {
  onUsage?: (u: UsagePayload) => void;
  onAuthStatus?: (a: AuthStatusPayload) => void;
  onSessions?: (s: SessionSummary[]) => void;
  /** Called when the connection opens or drops, so the UI can reflect state. */
  onConnectionChange?: (connected: boolean) => void;
};

/**
 * Subscribes to GET /api/events (SSE) with automatic reconnect and exponential
 * backoff. Node 18+ provides a streaming `fetch`, which we parse manually to
 * avoid depending on a global `EventSource` type. Call dispose() to stop.
 */
export class SseClient {
  private abort?: AbortController;
  private stopped = false;
  private backoffMs = 1000;
  private readonly maxBackoffMs = 30000;
  private reconnectTimer?: ReturnType<typeof setTimeout>;

  constructor(private readonly handlers: SseHandlers) {}

  start(): void {
    this.stopped = false;
    void this.connect();
  }

  /** Tear down and immediately reconnect (e.g. after a config change). */
  restart(): void {
    this.dispose();
    this.backoffMs = 1000;
    this.start();
  }

  dispose(): void {
    this.stopped = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = undefined;
    }
    this.abort?.abort();
    this.abort = undefined;
  }

  private scheduleReconnect(): void {
    if (this.stopped) {
      return;
    }
    this.handlers.onConnectionChange?.(false);
    const delay = this.backoffMs;
    this.backoffMs = Math.min(this.backoffMs * 2, this.maxBackoffMs);
    this.reconnectTimer = setTimeout(() => void this.connect(), delay);
  }

  private async connect(): Promise<void> {
    if (this.stopped) {
      return;
    }
    // Scope this attempt to its own controller. A later restart()/dispose()
    // replaces this.abort; we compare identity before acting so a superseded
    // in-flight stream tears down instead of firing handlers after teardown.
    const ctrl = new AbortController();
    this.abort = ctrl;
    try {
      const res = await fetch(`${baseUrl()}/api/events`, {
        signal: ctrl.signal,
        headers: { Accept: "text/event-stream" },
      });
      if (this.abort !== ctrl) {
        return; // superseded while the fetch was in flight
      }
      if (!res.ok || !res.body) {
        throw new Error(`HTTP ${res.status}`);
      }
      // Connected: reset backoff and notify.
      this.backoffMs = 1000;
      this.handlers.onConnectionChange?.(true);
      await this.readStream(res.body, ctrl);
      // Stream ended cleanly (server closed) -> reconnect, unless superseded.
      if (!this.stopped && this.abort === ctrl) {
        this.scheduleReconnect();
      }
    } catch (err) {
      if (this.stopped || this.abort !== ctrl) {
        return;
      }
      this.scheduleReconnect();
    }
  }

  private async readStream(body: ReadableStream<Uint8Array>, ctrl: AbortController): Promise<void> {
    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    while (!this.stopped && this.abort === ctrl) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      // SSE events are separated by a blank line.
      let sep: number;
      while ((sep = buffer.indexOf("\n\n")) !== -1) {
        const raw = buffer.slice(0, sep);
        buffer = buffer.slice(sep + 2);
        this.dispatch(raw);
      }
    }
  }

  private dispatch(raw: string): void {
    let event = "message";
    const dataLines: string[] = [];
    for (const line of raw.split("\n")) {
      if (line.startsWith("event:")) {
        event = line.slice(6).trim();
      } else if (line.startsWith("data:")) {
        dataLines.push(line.slice(5).trim());
      }
    }
    if (dataLines.length === 0) {
      return;
    }
    let payload: unknown;
    try {
      payload = JSON.parse(dataLines.join("\n"));
    } catch {
      return;
    }
    switch (event) {
      case "usage":
        this.handlers.onUsage?.(payload as UsagePayload);
        break;
      case "auth_status":
        this.handlers.onAuthStatus?.(payload as AuthStatusPayload);
        break;
      case "sessions":
        this.handlers.onSessions?.(payload as SessionSummary[]);
        break;
      // `update_available`, `ping` are ignored in v1.
    }
  }
}
