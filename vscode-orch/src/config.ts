import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';

const DEFAULT_REFRESH_SECONDS = 30;

export class OrchConfig {
  private refreshIntervalSeconds = DEFAULT_REFRESH_SECONDS;
  private showResolvedIssues = false;
  private runStatusFilter: string[] = [];
  private vaultPathSetting = '';
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
    const folder = vscode.workspace.workspaceFolders?.[0];
    return folder?.uri.fsPath;
  }

  async resolveVaultPath(): Promise<string | undefined> {
    const workspaceRoot = this.getWorkspaceRoot();
    if (!workspaceRoot) {
      return this.vaultPathSetting ? expandPath(this.vaultPathSetting, '') : undefined;
    }

    const orchDir = path.join(workspaceRoot, '.orch');
    try {
      if (fs.existsSync(orchDir) && fs.statSync(orchDir).isDirectory()) {
        const configPath = path.join(orchDir, 'config.yaml');
        if (fs.existsSync(configPath)) {
          const content = fs.readFileSync(configPath, 'utf8');
          const vaultValue = parseVaultFromConfig(content);
          if (vaultValue) {
            return expandPath(vaultValue, workspaceRoot);
          }
        }
      }
    } catch (error) {
      console.warn('orch: failed to inspect .orch/config.yaml', error);
    }

    if (this.vaultPathSetting) {
      return expandPath(this.vaultPathSetting, workspaceRoot);
    }

    return undefined;
  }

  private reload(): void {
    const config = vscode.workspace.getConfiguration('orch');
    this.refreshIntervalSeconds = config.get<number>('refreshInterval', DEFAULT_REFRESH_SECONDS);
    this.showResolvedIssues = config.get<boolean>('issues.showResolved', false);
    this.runStatusFilter = config.get<string[]>('runs.statusFilter', []) || [];
    this.vaultPathSetting = config.get<string>('vaultPath', '') || '';
  }
}

function parseVaultFromConfig(content: string): string | undefined {
  const lines = content.split(/\r?\n/);
  for (const line of lines) {
    const trimmed = stripComments(line).trim();
    if (!trimmed) {
      continue;
    }
    const match = trimmed.match(/^vault\s*:\s*(.*)$/);
    if (!match) {
      continue;
    }
    let value = match[1].trim();
    if (!value) {
      return undefined;
    }
    value = stripQuotes(value);
    return value || undefined;
  }
  return undefined;
}

function stripComments(line: string): string {
  const index = line.indexOf('#');
  if (index === -1) {
    return line;
  }
  return line.slice(0, index);
}

function stripQuotes(value: string): string {
  if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
    return value.slice(1, -1).trim();
  }
  return value;
}

function expandPath(input: string, baseDir: string): string {
  let expanded = input.trim();
  if (!expanded) {
    return expanded;
  }
  if (expanded.startsWith('~')) {
    expanded = path.join(os.homedir(), expanded.slice(1));
  }
  if (path.isAbsolute(expanded)) {
    return expanded;
  }
  if (!baseDir) {
    return expanded;
  }
  return path.resolve(baseDir, expanded);
}
