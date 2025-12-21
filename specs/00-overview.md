# orch CLI Spec v0.2

## 0. 目的

orch は「複数LLM CLI（claude/codex/gemini等）を、issue/run/event という統一語彙で運用する」ためのオーケストレーター。
中核は non-interactive（対話しない）。対話が必要な局面はイベント（question）として外部化し、answer と tick で再開する。

## 設計原則

1. **non-interactive がデフォルト**
   - 実行中に入力待ちはしない
   - 人間の判断が必要なら question event を追記して終了（blocked）

2. **真実は append-only events**
   - 既存イベントを書き換えない
   - 状態（status/phase等）はイベントから派生。frontmatterはキャッシュ可だが必須ではない

3. **UIはskin**
   - VSCode/fzf/tmuxは後付け
   - CLIは安定契約（互換性）として扱う

4. **PTY/TTY を自前で握らない**
   - 対話は tmux attach に委譲（画像コピペ等のため必須）
   - orch 自身は tmux new-session / send-keys / capture-pane を必要最小限のみ使用

## Spec Files

- [01-concepts.md](./01-concepts.md) - 用語定義
- [02-store.md](./02-store.md) - Knowledge Store仕様
- [03-commands.md](./03-commands.md) - CLIコマンド仕様
- [04-daemon.md](./04-daemon.md) - Daemon仕様
- [05-agent.md](./05-agent.md) - Agent adapter仕様
- [06-events.md](./06-events.md) - Eventフォーマット
- [07-config.md](./07-config.md) - 設定ファイル仕様

## 互換性ポリシー

- v0.x: 破壊的変更あり得るが、以下は極力維持
  - サブコマンド名
  - --json のトップレベルキー（ok/issue_id/run_id等）
  - TSV列順
- v1.0: RUN_REF、イベント形式、主要サブコマンドは固定
