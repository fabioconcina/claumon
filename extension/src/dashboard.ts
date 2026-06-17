import * as vscode from "vscode";
import { baseUrl, getConfig } from "./client";

/**
 * Singleton webview panel that embeds the live claumon browser UI in an iframe.
 * If the server is unreachable, shows a friendly offline message with Retry.
 */
export class Dashboard {
  private static current?: Dashboard;
  private readonly panel: vscode.WebviewPanel;
  private disposed = false;

  static show(): void {
    if (Dashboard.current) {
      Dashboard.current.panel.reveal();
      void Dashboard.current.update();
      return;
    }
    Dashboard.current = new Dashboard();
  }

  /** Refresh the open panel, if any (e.g. after a config change). */
  static refresh(): void {
    if (Dashboard.current) {
      void Dashboard.current.update();
    }
  }

  private constructor() {
    this.panel = vscode.window.createWebviewPanel(
      "claumonDashboard",
      "Claumon Dashboard",
      vscode.ViewColumn.Active,
      { enableScripts: true, retainContextWhenHidden: true },
    );
    this.panel.onDidDispose(() => {
      this.disposed = true;
      Dashboard.current = undefined;
    });
    this.panel.webview.onDidReceiveMessage((msg) => {
      if (msg?.type === "retry") {
        void this.update();
      } else if (msg?.type === "openExternal" && isLoopbackHost(getConfig().host)) {
        // Only ever open a loopback URL externally; never a host an untrusted
        // source could have pointed us at.
        void vscode.env.openExternal(vscode.Uri.parse(baseUrl()));
      }
    });
    void this.update();
  }

  private async update(): Promise<void> {
    if (this.disposed) {
      return;
    }
    // Defence in depth: even though host is machine-scoped, never embed or
    // script a non-loopback origin in a webview. Refuse anything else.
    if (!isLoopbackHost(getConfig().host)) {
      this.panel.webview.html = this.blockedHtml();
      return;
    }
    const url = baseUrl();
    const reachable = await this.ping(url);
    this.panel.webview.html = reachable ? this.embedHtml(url) : this.offlineHtml();
  }

  /** Shown when host is not loopback: we refuse to embed a remote origin. */
  private blockedHtml(): string {
    const safeHost = escapeHtml(getConfig().host);
    return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline';" />
  <style>
    body { font-family: var(--vscode-font-family); color: var(--vscode-foreground);
      display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; text-align: center; }
    .box { max-width: 30rem; padding: 2rem; }
    code { background: var(--vscode-textCodeBlock-background); padding: 0.1rem 0.3rem; border-radius: 3px; }
    p { color: var(--vscode-descriptionForeground); }
  </style>
</head>
<body>
  <div class="box">
    <h2>Dashboard blocked</h2>
    <p>claumon is configured to a non-local host (<code>${safeHost}</code>). For safety the dashboard is only embedded for loopback hosts (<code>localhost</code> / <code>127.0.0.1</code>).</p>
    <p>Set <code>claumon.host</code> back to a loopback address in your user settings.</p>
  </div>
</body>
</html>`;
  }

  private async ping(url: string): Promise<boolean> {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 3000);
    try {
      const res = await fetch(`${url}/api/info`, { signal: ctrl.signal });
      return res.ok;
    } catch {
      return false;
    } finally {
      clearTimeout(timer);
    }
  }

  private embedHtml(url: string): string {
    // url is built from user config (claumon.host/port); escape it before it
    // lands in the CSP content attribute and the iframe src so a stray quote
    // or angle bracket can't break out of the attribute or inject a directive.
    const safeUrl = escapeHtml(url);
    const csp = [
      "default-src 'none'",
      `frame-src ${safeUrl}`,
      "style-src 'unsafe-inline'",
    ].join("; ");
    return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <style>
    html, body { margin: 0; padding: 0; height: 100%; overflow: hidden; }
    iframe { border: 0; width: 100%; height: 100vh; display: block; }
  </style>
</head>
<body>
  <iframe src="${safeUrl}/" title="Claumon"></iframe>
</body>
</html>`;
  }

  private offlineHtml(): string {
    const { host, port } = getConfig();
    const safeHost = escapeHtml(host);
    const safePort = escapeHtml(String(port));
    const nonce = makeNonce();
    return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';" />
  <style>
    body {
      font-family: var(--vscode-font-family);
      color: var(--vscode-foreground);
      display: flex; align-items: center; justify-content: center;
      height: 100vh; margin: 0; text-align: center;
    }
    .box { max-width: 28rem; padding: 2rem; }
    h2 { margin: 0 0 0.5rem; }
    code { background: var(--vscode-textCodeBlock-background); padding: 0.1rem 0.3rem; border-radius: 3px; }
    button {
      margin-top: 1rem; padding: 0.5rem 1rem; cursor: pointer;
      color: var(--vscode-button-foreground);
      background: var(--vscode-button-background);
      border: none; border-radius: 3px; font-size: 0.9rem;
    }
    button:hover { background: var(--vscode-button-hoverBackground); }
    p { color: var(--vscode-descriptionForeground); }
  </style>
</head>
<body>
  <div class="box">
    <h2>claumon is not running</h2>
    <p>Could not reach the claumon server at <code>${safeHost}:${safePort}</code>.</p>
    <p>Start it (e.g. <code>./claumon</code>) and click Retry. The host/port can be changed in settings (<code>claumon.host</code> / <code>claumon.port</code>).</p>
    <button id="retry">Retry</button>
  </div>
  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    document.getElementById('retry').addEventListener('click', () => vscode.postMessage({ type: 'retry' }));
  </script>
</body>
</html>`;
  }
}

/** True only for loopback hosts the webview is allowed to embed/open. */
function isLoopbackHost(host: string): boolean {
  const h = host.trim().toLowerCase().replace(/^\[|\]$/g, ""); // strip IPv6 brackets
  return h === "localhost" || h === "::1" || /^127(\.\d{1,3}){3}$/.test(h);
}

const HTML_ESCAPES: Record<string, string> = {
  "&": "&amp;",
  "<": "&lt;",
  ">": "&gt;",
  '"': "&quot;",
  "'": "&#39;",
};

/** Escape a string for safe interpolation into HTML text or a quoted attribute. */
function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => HTML_ESCAPES[c]);
}

function makeNonce(): string {
  let text = "";
  const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789";
  for (let i = 0; i < 32; i++) {
    text += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return text;
}
