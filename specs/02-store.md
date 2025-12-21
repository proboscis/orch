# Knowledge Store

orch は「知識ベース」を抽象化して扱う。最初の実装は file backend（Obsidian vault）。

## 必須操作（インターフェース）

| 操作 | 説明 |
|------|------|
| `ResolveIssue(issue_id)` | IssueDoc（タイトル/本文/Frontmatter）を取得 |
| `ListIssues()` | 全issueを一覧取得 |
| `CreateRun(issue_id, run_id, metadata)` | RunDoc（パス含む）を作成 |
| `AppendEvent(run_ref, event_line)` | イベントを追記 |
| `ListRuns(filter)` | RunSummary一覧（status/phase/updated等は派生またはキャッシュ） |
| `GetRun(run_ref)` | RunDoc（events tail含む）を取得 |
| `GetRunByShortID(short_id)` | 6文字hexでRunを検索 |
| `GetLatestRun(issue_id)` | issueの最新runを取得 |

## File Backend

### Issue検出

- vault内の任意の`.md`ファイルで、frontmatterに `type: issue` を持つものがissue
- `id` フィールドでissue IDを指定（省略時はファイル名）
- 配置場所は自由（`issues/` ディレクトリ不要）

### Issue frontmatter例

```yaml
---
type: issue
id: plc-123
title: Fix login timeout
status: open
---
```

### ディレクトリ構造

```
vault/
  任意の場所/<任意>.md   # type: issue frontmatterでissue判定
  runs/<ISSUE_ID>/<RUN_ID>.md
  runs/<ISSUE_ID>/<RUN_ID>.log/   # 任意：ログ格納用
  .orch/
    daemon.pid      # daemon PID
    daemon.log      # daemon ログ
    daemon.sock     # （将来）IPC用Unix socket
```

※ ObsidianはUI。vaultはただのファイル集合。

## 将来のBackend

- `github` - GitHub Issues/PRをバックエンドに
- `linear` - Linear issuesをバックエンドに
