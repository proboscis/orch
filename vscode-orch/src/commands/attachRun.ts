import * as vscode from 'vscode';
import { RunTreeItem } from '../providers/runsProvider';
import { OrchConfig } from '../config';
import { TerminalManager } from '../terminalManager';

export function registerAttachRunCommand(
  config: OrchConfig,
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
      const vaultPath = await config.resolveVaultPath();
      const vaultArg = vaultPath ? ` --vault ${quoteArg(vaultPath)}` : '';
      result.terminal.sendText(`orch${vaultArg} attach ${runRef}`);
    }
  });
}

function quoteArg(value: string): string {
  if (!value.includes(' ') && !value.includes('\"')) {
    return value;
  }
  return `\"${value.replace(/\"/g, '\\\\\"')}\"`;
}
