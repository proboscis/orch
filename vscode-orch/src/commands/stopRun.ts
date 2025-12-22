import * as vscode from 'vscode';
import { OrchClient } from '../orch/client';
import { RunTreeItem, RunsProvider } from '../providers/runsProvider';

export function registerStopRunCommand(
  client: OrchClient,
  runsProvider: RunsProvider
): vscode.Disposable {
  return vscode.commands.registerCommand('orch.run.stop', async (item?: RunTreeItem) => {
    if (!item) {
      vscode.window.showInformationMessage('Select a run from the Orch Runs panel.');
      return;
    }

    const runRef = `${item.run.issue_id}#${item.run.run_id}`;
    const choice = await vscode.window.showWarningMessage(
      `Stop run ${runRef}?`,
      { modal: true },
      'Stop'
    );
    if (choice !== 'Stop') {
      return;
    }

    try {
      await client.stopRun(runRef);
      vscode.window.showInformationMessage(`Stopped run ${runRef}`);
      runsProvider.refresh();
    } catch (error) {
      console.warn('orch: failed to stop run', error);
    }
  });
}
