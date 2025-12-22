import * as vscode from 'vscode';
import { OrchClient, IssueInfo } from '../orch/client';
import { OrchConfig } from '../config';

export class IssuesProvider implements vscode.TreeDataProvider<IssueTreeItem> {
  private readonly changeEmitter = new vscode.EventEmitter<void>();

  constructor(private readonly client: OrchClient, private readonly config: OrchConfig) {}

  get onDidChangeTreeData(): vscode.Event<void> {
    return this.changeEmitter.event;
  }

  refresh(): void {
    this.client.invalidateIssues();
    this.changeEmitter.fire();
  }

  getTreeItem(element: IssueTreeItem): vscode.TreeItem {
    return element;
  }

  async getChildren(): Promise<IssueTreeItem[]> {
    const includeResolved = this.config.getShowResolvedIssues();
    const issues = await this.client.listIssues(includeResolved);
    return issues.map((issue) => new IssueTreeItem(issue));
  }
}

export class IssueTreeItem extends vscode.TreeItem {
  readonly issue: IssueInfo;

  constructor(issue: IssueInfo) {
    super(buildLabel(issue), vscode.TreeItemCollapsibleState.None);
    this.issue = issue;
    this.contextValue = 'orchIssue';
    this.description = buildDescription(issue);
    this.tooltip = buildTooltip(issue);
    this.iconPath = iconForStatus(issue.status);
    this.command = {
      command: 'orch.issue.open',
      title: 'Open Issue',
      arguments: [this]
    };
  }
}

function buildLabel(issue: IssueInfo): string {
  const detail = issue.summary || issue.title || '';
  if (!detail) {
    return issue.id;
  }
  return `${issue.id} - ${detail}`;
}

function buildDescription(issue: IssueInfo): string {
  if (issue.summary && issue.summary !== issue.title) {
    return issue.title || '';
  }
  return '';
}

function buildTooltip(issue: IssueInfo): string {
  const parts = [issue.title || issue.summary || issue.id, `Status: ${issue.status}`];
  return parts.filter(Boolean).join('\n');
}

function iconForStatus(status: string): vscode.ThemeIcon {
  switch (status) {
    case 'resolved':
      return new vscode.ThemeIcon('issue-closed', new vscode.ThemeColor('charts.green'));
    case 'closed':
      return new vscode.ThemeIcon('issue-closed', new vscode.ThemeColor('charts.gray'));
    default:
      return new vscode.ThemeIcon('issue-opened', new vscode.ThemeColor('charts.blue'));
  }
}
