import * as vscode from 'vscode';
import { IssueTreeItem } from '../providers/issuesProvider';

export function registerOpenIssueCommand(): vscode.Disposable {
  return vscode.commands.registerCommand('orch.issue.open', async (item?: IssueTreeItem) => {
    if (!item) {
      vscode.window.showInformationMessage('Select an issue from the Orch Issues panel.');
      return;
    }

    const issuePath = item.issue.path;
    if (!issuePath) {
      vscode.window.showErrorMessage('Issue path not available.');
      return;
    }

    const uri = vscode.Uri.file(issuePath);
    await vscode.commands.executeCommand('vscode.open', uri);
  });
}
