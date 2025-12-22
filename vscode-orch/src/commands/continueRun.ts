import * as vscode from 'vscode';
import { OrchClient } from '../orch/client';
import { IssueTreeItem } from '../providers/issuesProvider';
import { RunsProvider } from '../providers/runsProvider';
import { pickAgent, pickBranch } from './pickers';

export function registerContinueRunCommand(
  client: OrchClient,
  runsProvider: RunsProvider
): vscode.Disposable {
  return vscode.commands.registerCommand('orch.issue.continueRun', async (item?: IssueTreeItem) => {
    if (!item) {
      vscode.window.showInformationMessage('Select an issue from the Orch Issues panel.');
      return;
    }

    const issueId = item.issue.id;
    const branches = await client.listBranches(issueId);
    const branch = await pickBranch(branches);
    if (!branch) {
      return;
    }

    const agent = await pickAgent();
    if (!agent) {
      return;
    }

    try {
      const result = await client.continueRun(issueId, branch, agent);
      vscode.window.showInformationMessage(`Continued run ${result.issue_id}#${result.run_id}`);
      runsProvider.refresh();
    } catch (error) {
      console.warn('orch: failed to continue run', error);
    }
  });
}
