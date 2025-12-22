import * as vscode from 'vscode';

export class TerminalManager implements vscode.Disposable {
  private readonly terminals = new Map<string, vscode.Terminal>();
  private readonly closeListener: vscode.Disposable;

  constructor(private readonly workspaceRoot?: string) {
    this.closeListener = vscode.window.onDidCloseTerminal((terminal) => {
      for (const [key, value] of this.terminals.entries()) {
        if (value === terminal) {
          this.terminals.delete(key);
          break;
        }
      }
    });
  }

  dispose(): void {
    this.closeListener.dispose();
    this.terminals.clear();
  }

  getOrCreate(runRef: string): { terminal: vscode.Terminal; created: boolean } {
    const existing = this.terminals.get(runRef);
    if (existing) {
      return { terminal: existing, created: false };
    }

    const terminal = vscode.window.createTerminal({
      name: `orch: ${runRef}`,
      cwd: this.workspaceRoot
    });
    this.terminals.set(runRef, terminal);
    return { terminal, created: true };
  }
}
