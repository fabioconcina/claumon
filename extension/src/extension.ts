import * as vscode from "vscode";
import { SseClient, getUsage, getSessions } from "./client";
import { StatusBar } from "./statusBar";
import { Dashboard } from "./dashboard";

let statusBar: StatusBar | undefined;
let sse: SseClient | undefined;

export function activate(context: vscode.ExtensionContext): void {
  statusBar = new StatusBar();
  const bar = statusBar;

  sse = new SseClient({
    onUsage: (u) => bar.setUsage(u),
    onAuthStatus: (a) => bar.setAuth(a),
    onSessions: (s) => bar.setSessions(s),
    onConnectionChange: (connected) => bar.setConnected(connected),
  });
  sse.start();

  // Initial paint before the first SSE tick arrives. Sessions only stream on a
  // file change, so seed the model from a one-shot fetch too.
  void getUsage()
    .then((u) => bar.setUsage(u))
    .catch(() => {
      /* offline; SSE client will report state */
    });
  void getSessions()
    .then((s) => bar.setSessions(s))
    .catch(() => {
      /* offline; sessions will arrive over SSE once connected */
    });

  context.subscriptions.push(
    vscode.commands.registerCommand("claumon.openDashboard", () => Dashboard.show()),
    vscode.commands.registerCommand("claumon.reconnect", () => {
      sse?.restart();
      void getUsage()
        .then((u) => bar.setUsage(u))
        .catch(() => {
          /* still offline */
        });
      void getSessions()
        .then((s) => bar.setSessions(s))
        .catch(() => {
          /* still offline */
        });
      Dashboard.refresh();
    }),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (!e.affectsConfiguration("claumon")) {
        return;
      }
      // Host/port/metric may have changed: re-render and reconnect.
      bar.refresh();
      if (
        e.affectsConfiguration("claumon.host") ||
        e.affectsConfiguration("claumon.port")
      ) {
        sse?.restart();
        Dashboard.refresh();
      }
    }),
    { dispose: () => sse?.dispose() },
    bar,
  );
}

export function deactivate(): void {
  sse?.dispose();
  statusBar?.dispose();
}
