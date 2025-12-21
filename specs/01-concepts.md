# 用語

| 用語 | 説明 |
|------|------|
| Issue | 仕様の単位。frontmatterに `type: issue` を持つ.mdファイル。IDで一意（例: plc124） |
| Run | 実行の単位。同一Issueから複数Runが発生する |
| Event | Runに追記される1行レコード（append-only） |
| RUN_REF | `ISSUE_ID#RUN_ID` 形式（または ISSUE_ID だけ＝最新run） |
| SHORT_ID | Runの6文字hex識別子（git風）。RUN_REFの代わりに使用可能 |

## Issue

Issueはvault内の任意の場所に配置できる。検出条件:

- `.md` ファイルである
- frontmatterに `type: issue` を持つ

### Issue frontmatter

```yaml
---
type: issue          # 必須: issueとして認識される
id: plc-123          # 任意: issue ID（省略時はファイル名）
title: Fix login     # 任意: タイトル（省略時は最初の # heading）
status: open         # 任意: ユーザー定義のステータス
priority: high       # 任意: その他のメタデータ
---
```

## Run

Runは `runs/<ISSUE_ID>/<RUN_ID>.md` に格納される。

### RUN_REF

Runを参照する形式:
- `ISSUE_ID#RUN_ID` - 特定のrun
- `ISSUE_ID` - 最新のrun
- `SHORT_ID` - 6文字hex（例: `31909e`）

### SHORT_ID

RunのSHORT_IDはissue IDとrun IDから生成される6文字のhex:
```
SHA256(ISSUE_ID + "#" + RUN_ID)[:6]
```

## Status

Runのステータス（eventsから派生）:

| Status | 説明 |
|--------|------|
| queued | 作成直後、まだ起動していない |
| booting | agent起動中 |
| running | agent実行中 |
| blocked | 入力待ち（question未回答） |
| pr_open | PR作成済み、レビュー待ち |
| done | 正常完了 |
| failed | エラー終了 |
| canceled | ユーザーによる中止 |
| unknown | agentが予期せず終了（shell prompt検出） |
