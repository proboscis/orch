# Agent Adapter

orch は "LLMの状態"を直接取らない。agentは単なる外部プロセスとして扱う。
daemonがtmuxセッションを監視し、状態を推定する。

## 起動時の環境変数

agent起動時に以下の環境変数が設定される:

| 変数 | 説明 |
|------|------|
| `ORCH_ISSUE_ID` | Issue ID |
| `ORCH_RUN_ID` | Run ID |
| `ORCH_RUN_PATH` | Run document path |
| `ORCH_WORKTREE_PATH` | Git worktree path |
| `ORCH_BRANCH` | Git branch name |
| `ORCH_VAULT` | Vault root path |

## プロンプト渡し

agent起動時、issue本文をプロンプトとして渡す:

| Agent | 起動方法 |
|-------|---------|
| claude | `claude "prompt..."` |
| codex | 各CLIの規約に従う |
| gemini | 各CLIの規約に従う |

## 状態更新

- agentは自発的に状態更新しなくてよい（daemonが監視）
- agentが明示的に状態を変えたい場合は `orch event append ...` を呼ぶ（将来）

## サポートAgent

### claude (Claude Code)

```bash
claude --dangerously-skip-permissions "prompt..."
```

### codex (予約)

```bash
codex "prompt..."
```

### gemini (予約)

```bash
gemini "prompt..."
```

### custom

`--agent-cmd` で任意のコマンドを指定:

```bash
orch run ISSUE --agent custom --agent-cmd "my-agent run"
```
