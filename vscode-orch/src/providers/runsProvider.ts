import * as vscode from 'vscode';
import { OrchClient, RunInfo } from '../orch/client';
import { OrchConfig } from '../config';

export class RunsProvider implements vscode.TreeDataProvider<RunTreeItem> {
  private readonly changeEmitter = new vscode.EventEmitter<void>();

  constructor(private readonly client: OrchClient, private readonly config: OrchConfig) {}

  get onDidChangeTreeData(): vscode.Event<void> {
    return this.changeEmitter.event;
  }

  refresh(): void {
    this.client.invalidateRuns();
    this.changeEmitter.fire();
  }

  getTreeItem(element: RunTreeItem): vscode.TreeItem {
    return element;
  }

  async getChildren(): Promise<RunTreeItem[]> {
    const statusFilter = this.config.getRunStatusFilter();
    const runs = await this.client.listRuns(statusFilter);
    return runs.map((run) => new RunTreeItem(run));
  }
}

export class RunTreeItem extends vscode.TreeItem {
  readonly run: RunInfo;

  constructor(run: RunInfo) {
    super(formatLabel(run), vscode.TreeItemCollapsibleState.None);
    this.run = run;
    this.contextValue = 'orchRun';
    this.description = formatDescription(run);
    this.tooltip = formatTooltip(run);
    this.iconPath = iconForStatus(run.status);
    this.command = {
      command: 'orch.run.attach',
      title: 'Attach to Run',
      arguments: [this]
    };
  }
}

function formatLabel(run: RunInfo): string {
  const shortId = run.short_id || run.run_id;
  return `${run.issue_id}#${shortId}`;
}

function formatDescription(run: RunInfo): string {
  const updated = run.updated_ago ? ` | ${run.updated_ago}` : '';
  return `${run.status}${updated}`;
}

function formatTooltip(run: RunInfo): string {
  const lines = [
    `${run.issue_id}#${run.run_id}`,
    `Status: ${run.status}`,
    run.agent ? `Agent: ${run.agent}` : '',
    run.branch ? `Branch: ${run.branch}` : '',
    run.updated_at ? `Updated: ${run.updated_at}` : ''
  ];
  return lines.filter(Boolean).join('\n');
}

function iconForStatus(status: string): vscode.ThemeIcon {
  switch (status) {
    case 'running':
      return new vscode.ThemeIcon('play-circle', new vscode.ThemeColor('charts.green'));
    case 'blocked':
    case 'blocked_api':
      return new vscode.ThemeIcon('warning', new vscode.ThemeColor('charts.yellow'));
    case 'queued':
    case 'booting':
      return new vscode.ThemeIcon('clock', new vscode.ThemeColor('charts.blue'));
    case 'pr_open':
      return new vscode.ThemeIcon('git-pull-request', new vscode.ThemeColor('charts.blue'));
    case 'done':
      return new vscode.ThemeIcon('check', new vscode.ThemeColor('charts.green'));
    case 'failed':
      return new vscode.ThemeIcon('error', new vscode.ThemeColor('charts.red'));
    case 'canceled':
      return new vscode.ThemeIcon('circle-slash', new vscode.ThemeColor('charts.gray'));
    case 'unknown':
      return new vscode.ThemeIcon('question', new vscode.ThemeColor('charts.orange'));
    default:
      return new vscode.ThemeIcon('circle-outline');
  }
}
