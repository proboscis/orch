import * as vscode from 'vscode';
import { OrchConfig } from './config';
import { OrchClient } from './orch/client';
import { IssuesProvider } from './providers/issuesProvider';
import { RunsProvider } from './providers/runsProvider';
import { registerOpenIssueCommand } from './commands/openIssue';
import { registerStartRunCommand } from './commands/startRun';
import { registerContinueRunCommand } from './commands/continueRun';
import { registerAttachRunCommand } from './commands/attachRun';
import { registerStopRunCommand } from './commands/stopRun';
import { registerResolveRunCommand } from './commands/resolveRun';
import { TerminalManager } from './terminalManager';

export function activate(context: vscode.ExtensionContext): void {
  const config = new OrchConfig();
  const client = new OrchClient(config);
  const issuesProvider = new IssuesProvider(client, config);
  const runsProvider = new RunsProvider(client, config);
  const terminalManager = new TerminalManager(config.getWorkspaceRoot());

  context.subscriptions.push(
    config,
    terminalManager,
    vscode.window.registerTreeDataProvider('orchIssues', issuesProvider),
    vscode.window.registerTreeDataProvider('orchRuns', runsProvider),
    registerOpenIssueCommand(),
    registerStartRunCommand(client, runsProvider),
    registerContinueRunCommand(client, runsProvider),
    registerAttachRunCommand(config, terminalManager),
    registerStopRunCommand(client, runsProvider),
    registerResolveRunCommand(client, runsProvider),
    vscode.commands.registerCommand('orch.refreshIssues', () => issuesProvider.refresh()),
    vscode.commands.registerCommand('orch.refreshRuns', () => runsProvider.refresh()),
    vscode.commands.registerCommand('orch.refreshAll', () => {
      issuesProvider.refresh();
      runsProvider.refresh();
    })
  );

  let refreshTimer = startRefreshTimer(config, issuesProvider, runsProvider);
  context.subscriptions.push({
    dispose: () => {
      if (refreshTimer) {
        clearInterval(refreshTimer);
      }
    }
  });

  context.subscriptions.push(
    config.onDidChange(() => {
      if (refreshTimer) {
        clearInterval(refreshTimer);
      }
      refreshTimer = startRefreshTimer(config, issuesProvider, runsProvider);
      issuesProvider.refresh();
      runsProvider.refresh();
    })
  );
}

export function deactivate(): void {}

function startRefreshTimer(
  config: OrchConfig,
  issuesProvider: IssuesProvider,
  runsProvider: RunsProvider
): NodeJS.Timeout | undefined {
  const interval = config.getRefreshIntervalMs();
  if (interval <= 0) {
    return undefined;
  }
  return setInterval(() => {
    issuesProvider.refresh();
    runsProvider.refresh();
  }, interval);
}
