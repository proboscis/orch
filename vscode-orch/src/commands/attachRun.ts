import * as vscode from 'vscode';
import { RunTreeItem } from '../providers/runsProvider';
import { TerminalManager } from '../terminalManager';

export function registerAttachRunCommand(
  terminalManager: TerminalManager
): vscode.Disposable {
  return vscode.commands.registerCommand('orch.run.attach', async (item?: RunTreeItem) => {
    if (!item) {
      vscode.window.showInformationMessage('Select a run from the Orch Runs panel.');
      return;
    }

    const runRef = `${item.run.issue_id}#${item.run.run_id}`;
    const result = terminalManager.getOrCreate(runRef);
    result.terminal.show(true);

    if (result.created) {
      result.terminal.sendText(`orch attach ${runRef}`);
    }
  });
}
