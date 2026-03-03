import * as path from 'path';
import * as vscode from 'vscode';

const DEFAULT_REFRESH_SECONDS = 30;

export class OrchConfig {
  private refreshIntervalSeconds = DEFAULT_REFRESH_SECONDS;
  private showResolvedIssues = false;
  private runStatusFilter: string[] = [];
  private projectRootSetting = '';
  private readonly changeEmitter = new vscode.EventEmitter<void>();
  private readonly configListener: vscode.Disposable;

  constructor() {
    this.reload();
    this.configListener = vscode.workspace.onDidChangeConfiguration((event) => {
      if (event.affectsConfiguration('orch')) {
        this.reload();
        this.changeEmitter.fire();
      }
    });
  }

  dispose(): void {
    this.configListener.dispose();
    this.changeEmitter.dispose();
  }

  get onDidChange(): vscode.Event<void> {
    return this.changeEmitter.event;
  }

  getRefreshIntervalMs(): number {
    const seconds = Number.isFinite(this.refreshIntervalSeconds)
      ? this.refreshIntervalSeconds
      : DEFAULT_REFRESH_SECONDS;
    return Math.max(0, Math.floor(seconds * 1000));
  }

  getShowResolvedIssues(): boolean {
    return this.showResolvedIssues;
  }

  getRunStatusFilter(): string[] {
    return [...this.runStatusFilter];
  }

  getWorkspaceRoot(): string | undefined {
    if (this.projectRootSetting) {
      return expandPath(this.projectRootSetting, '');
    }
    const folder = vscode.workspace.workspaceFolders?.[0];
    return folder?.uri.fsPath;
  }

  private reload(): void {
    const config = vscode.workspace.getConfiguration('orch');
    this.refreshIntervalSeconds = config.get<number>('refreshInterval', DEFAULT_REFRESH_SECONDS);
    this.showResolvedIssues = config.get<boolean>('issues.showResolved', false);
    this.runStatusFilter = config.get<string[]>('runs.statusFilter', []) || [];
    this.projectRootSetting = config.get<string>('projectRoot', '') || '';
  }
}

function expandPath(input: string, baseDir: string): string {
  let expanded = input.trim();
  if (!expanded) {
    return expanded;
  }
  if (path.isAbsolute(expanded)) {
    return expanded;
  }
  if (!baseDir) {
    return expanded;
  }
  return path.resolve(baseDir, expanded);
}
