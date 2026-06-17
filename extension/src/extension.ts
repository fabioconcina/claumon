import * as vscode from "vscode";
import { SseClient, getUsage } from "./client";
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
    onConnectionChange: (connected) => bar.setConnected(connected),
  });
  sse.start();

  // Initial paint before the first SSE tick arrives.
  void getUsage()
    .then((u) => bar.setUsage(u))
    .catch(() => {
      /* offline; SSE client will report state */
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
