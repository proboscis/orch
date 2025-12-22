import * as vscode from 'vscode';

export async function pickAgent(): Promise<string | undefined> {
  const options: vscode.QuickPickItem[] = [
    { label: 'claude', description: 'Anthropic Claude' },
    { label: 'codex', description: 'OpenAI Codex' },
    { label: 'gemini', description: 'Google Gemini' }
  ];

  const pick = await vscode.window.showQuickPick(options, {
    placeHolder: 'Select an agent'
  });
  return pick?.label;
}

export async function pickBranch(branches: string[]): Promise<string | undefined> {
  if (branches.length === 0) {
    return vscode.window.showInputBox({
      prompt: 'Enter branch name to continue from',
      placeHolder: 'issue/<id>/run-<run-id>'
    });
  }

  const manualLabel = 'Enter branch name...';
  const items: vscode.QuickPickItem[] = branches.map((branch) => ({ label: branch }));
  items.push({ label: manualLabel });

  const pick = await vscode.window.showQuickPick(items, {
    placeHolder: 'Select a branch to continue from'
  });

  if (!pick) {
    return undefined;
  }

  if (pick.label === manualLabel) {
    return vscode.window.showInputBox({
      prompt: 'Enter branch name to continue from',
      placeHolder: 'issue/<id>/run-<run-id>'
    });
  }

  return pick.label;
}
