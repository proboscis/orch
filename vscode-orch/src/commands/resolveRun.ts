import * as vscode from 'vscode';
import { OrchClient } from '../orch/client';
import { RunTreeItem, RunsProvider } from '../providers/runsProvider';

export function registerResolveRunCommand(
  client: OrchClient,
  runsProvider: RunsProvider
): vscode.Disposable {
  return vscode.commands.registerCommand('orch.run.resolve', async (item?: RunTreeItem) => {
    if (!item) {
      vscode.window.showInformationMessage('Select a run from the Orch Runs panel.');
      return;
    }

    const runRef = `${item.run.issue_id}#${item.run.run_id}`;
    const choice = await vscode.window.showWarningMessage(
      `Resolve run ${runRef}?`,
      { modal: true },
      'Resolve'
    );
    if (choice !== 'Resolve') {
      return;
    }

    try {
      await client.resolveRun(runRef);
      vscode.window.showInformationMessage(`Resolved run ${runRef}`);
      runsProvider.refresh();
    } catch (error) {
      console.warn('orch: failed to resolve run', error);
    }
  });
}
