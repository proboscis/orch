import * as vscode from 'vscode';
import { OrchClient } from '../orch/client';
import { IssueTreeItem } from '../providers/issuesProvider';
import { RunsProvider } from '../providers/runsProvider';
import { pickAgent } from './pickers';

export function registerStartRunCommand(
  client: OrchClient,
  runsProvider: RunsProvider
): vscode.Disposable {
  return vscode.commands.registerCommand('orch.issue.startRun', async (item?: IssueTreeItem) => {
    if (!item) {
      vscode.window.showInformationMessage('Select an issue from the Orch Issues panel.');
      return;
    }

    const agent = await pickAgent();
    if (!agent) {
      return;
    }

    try {
      const result = await client.startRun(item.issue.id, agent);
      vscode.window.showInformationMessage(`Started run ${result.issue_id}#${result.run_id}`);
      runsProvider.refresh();
    } catch (error) {
      console.warn('orch: failed to start run', error);
    }
  });
}
