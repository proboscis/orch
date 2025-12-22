# CLI Commands

## 共通オプション（全コマンド）

| オプション | 説明 |
|-----------|------|
| `--vault PATH` | vault path（または env `ORCH_VAULT`） |
| `--backend file\|github\|linear` | v0.2では file を正式、他は予約 |
| `--json` | 機械可読JSON出力 |
| `--tsv` | fzf向け出力（ps等で有効） |
| `--quiet` | 人間向け出力を抑制 |
| `--log-level` | error\|warn\|info\|debug |

## 終了コード

| Code | 意味 |
|------|------|
| 0 | 成功 |
| 2 | issue not found |
| 3 | worktree error |
| 4 | tmux error |
| 5 | agent launch error |
| 6 | run not found |
| 7 | question not found |
| 10 | internal error |

---

## orch run ISSUE_ID

新しいrunを作成し、worktreeを作成し、agentを起動する（即return）

### オプション

| オプション | 説明 |
|-----------|------|
| `--new` | 常に新run（デフォルト） |
| `--reuse` | 最新runを再開（blocked向け） |
| `--run-id <RUN_ID>` | 手動指定 |
| `--agent claude\|codex\|gemini\|custom:` | agent種別 |
| `--agent-cmd` | custom時の起動コマンド |
| `--base-branch main` | デフォルトmain |
| `--branch` | 省略時は規約生成 |
| `--worktree-root` | 例: .git-worktrees |
| `--repo-root` | git rootを明示（省略時は探索） |
| `--tmux / --no-tmux` | デフォルトtmux |
| `--tmux-session` | 省略時は規約生成 |
| `--dry-run` | 副作用なし：作成予定を表示 |

### 規約（デフォルト）

- RUN_ID = `YYYYMMDD-HHMMSS`
- branch = `issue/<ISSUE_ID>/run-<RUN_ID>`
- worktree_path = `<worktree_root>/<ISSUE_ID>/<RUN_ID>`
- tmux_session = `run-<ISSUE_ID>-<RUN_ID>`

### 副作用

- Run doc作成
- Event追記: status=queued/booting/running, artifact(worktree/branch/session) 等
- git worktree add + checkout
- tmux new-session で agent起動（非対話モード）

---

## orch ps

runs一覧を表示（人間/機械）

### オプション

| オプション | 説明 |
|-----------|------|
| `--status` | queued,booting,running,blocked,blocked_api,pr_open,done,resolved,failed,canceled,unknown |
| `--issue-status` | open,closed,etc |
| `--issue <ISSUE_ID>` | 特定issueのrunのみ |
| `--limit N` | default 50 |
| `--sort updated\|started` | default updated |
| `--since <timestamp>` | 指定日時以降 |
| `--absolute-time` | 相対時間ではなく絶対時刻で表示 |
| `--all` | resolved を含めて表示 |

### TSV列（固定順）

```
issue_id, issue_status, run_id, short_id, agent, status, updated_at, pr_url, branch, worktree_path, tmux_session
```

---

## orch show RUN_REF

1runの詳細（events tail、未回答question、主要artifact）

### オプション

| オプション | 説明 |
|-----------|------|
| `--tail N` | default 80 |
| `--questions` | 未回答のみ |
| `--events-only` | イベントだけ |

---

## orch attach RUN_REF

tmux attach（画像コピペ等の手動対話）

### オプション

| オプション | 説明 |
|-----------|------|
| `--pane log\|shell` | 予約。v0.2ではセッションattachのみ |
| `--window` | 任意 |

### 挙動

- セッションが存在すればattach
- セッションが無い場合、worktreeが存在すれば自動作成してattach
- worktreeも無い場合はエラー

---

## orch tick RUN_REF | --all

blocked等のrunを再開するトリガ（質問が解消されていれば次フェーズを進める）

### オプション

| オプション | 説明 |
|-----------|------|
| `--only-blocked` | default on when --all |
| `--agent …` | 再開時のagent指定 |
| `--max N` | --all時の最大処理件数 |

### 挙動

- runのeventsを読み、未回答questionが無ければ agent を再起動（新window推奨）
- 未回答があれば何もしない

---

## orch open ISSUE_ID|RUN_REF

Obsidian/Editorで該当ノートを開く

### オプション

| オプション | 説明 |
|-----------|------|
| `--app obsidian\|editor\|default` | アプリ指定 |
| `--print-path` | 開かずにパスだけ返す |

---

## orch stop ISSUE_ID | ISSUE_ID#RUN_ID | --all

実行中のrunを停止する

### 挙動

- ISSUE_ID のみ指定 → そのissueの全アクティブrun（running/booting/blocked/queued）を停止
- ISSUE_ID#RUN_ID 指定 → 特定runのみ停止
- --all → 全アクティブrunを停止

### 停止処理

- tmuxセッションが存在すれば kill-session
- status=canceled イベントを追記

### オプション

| オプション | 説明 |
|-----------|------|
| `--all` | 全runを停止 |
| `--force` | セッションが無くても強制的にcanceled化 |

---

## orch repair

システム状態を修復する（最終手段）

### 挙動

- daemonが異常なら再起動
- "running"だがtmuxセッションが無いrunを検出 → failed化
- orphanedなworktree/sessionを検出（警告のみ）
- 矛盾した状態を修正

### オプション

| オプション | 説明 |
|-----------|------|
| `--dry-run` | 修復せず問題を報告のみ |
| `--force` | 確認なしで修復実行 |

---

## orch issue create ISSUE_ID

新しいissueを作成

### オプション

| オプション | 説明 |
|-----------|------|
| `--title "…"` | タイトル |
| `--body "…"` | 本文 |
| `--edit` | 作成後$EDITORで開く |

### 副作用

- `issues/<ISSUE_ID>.md` を作成
- frontmatterに `type: issue`, `id`, `title`, `status: open` を設定

---

## orch issue list

vault内の全issueを一覧表示

### オプション

| オプション | 説明 |
|-----------|------|
| `--status <status>` | 特定statusのissueのみ（open/closed等） |
| `--with-runs` | 各issueのアクティブrunも表示 |

### 挙動

- vault全体をスキャンし、`type: issue` frontmatterを持つファイルを検出
- 表示項目:
  - id: issue ID
  - title: タイトル
  - status: frontmatterの `status` フィールド（open/closed等）
  - runs: アクティブなrun数とその状態サマリ

### 出力例

```
ID          STATUS  TITLE                           RUNS
plc-123     open    Fix login timeout               1 running
plc-124     open    Add dark mode                   1 blocked, 1 done
plc-125     closed  Update documentation            -
```

### JSON出力

```json
{
  "ok": true,
  "issues": [
    {
      "id": "plc-123",
      "title": "Fix login timeout",
      "status": "open",
      "path": "/vault/issues/plc-123.md",
      "runs": [
        {"run_id": "20251221-123456", "status": "running"}
      ]
    }
  ]
}
```
