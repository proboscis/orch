# Daemon

## 概要

orchは自動的にバックグラウンドdaemonを起動・管理する。ユーザーはdaemonの存在を意識する必要がない。

## ライフサイクル

- 任意のorch コマンド実行時、daemonが起動していなければ自動起動
- daemonはidempotent（多重起動しない）
- PIDファイル: `$VAULT/.orch/daemon.pid`
- ログファイル: `$VAULT/.orch/daemon.log`

## 監視ループ（5-10秒間隔）

各"running"/"blocked"/"unknown"状態のrunに対して:

1. tmuxセッション存在確認
2. capture-paneで最新出力を取得
3. 状態判定（下記参照）

## 状態判定ロジック

claude-squad互換のロジック:

1. **Agent Exit検出**: Claude UIパターンが無く、shell promptが表示 → `unknown`
2. **完了パターン**: "task completed successfully" 等 → `done`
3. **エラーパターン**: "fatal error" 等 → `failed`
4. **Content変化あり**: 主要コンテンツが変化 → `running`
5. **Content安定 + Prompt検出**: 入力待ちパターン → `blocked`
6. **その他**: 状態維持

### Content変化検出

- 出力全体ではなく、status bar（最後5行）を除いた部分をhash
- token counterの更新で誤検出しないため

### Prompt検出パターン

```
"No, and tell Claude what to do differently"
"tell Claude what to do differently"
"↵ send"
"? for shortcuts"
"accept edits"
"bypass permissions"
"shift+tab to cycle"
"Esc to cancel"
"to show all projects"
```

### Agent Exit検出

1. Claude UIパターン（"↵ send", "tokens" 等）が存在すれば → agentは生存
2. 最終行がshell prompt（`$`, `%`, `❯`, `git:(...)` 等）→ agent終了

## ファイル構造

```
vault/.orch/
  daemon.pid      # daemon PID
  daemon.log      # daemon ログ
  daemon.sock     # （将来）IPC用Unix socket
```
