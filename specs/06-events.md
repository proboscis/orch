# Event Format

Run本文に箇条書きで追記する。1行 = 1イベント。Dataview friendly。

## 形式

```
- <ts> | <type> | <name> | key=value | key=value …
```

- `ts`: ISO8601（例: `2025-12-20T11:45:10+09:00`）

## 推奨 type/name

### status

Run状態の変更:

| name | 説明 |
|------|------|
| queued | 作成直後 |
| booting | agent起動中 |
| running | 実行中 |
| blocked | 入力待ち |
| pr_open | PR作成済み |
| done | 正常完了 |
| failed | エラー終了 |
| canceled | 中止 |
| unknown | agent予期せず終了 |

### phase

作業フェーズ:

| name | 説明 |
|------|------|
| plan | 計画策定 |
| implement | 実装 |
| test | テスト |
| pr | PR作成 |
| review | レビュー対応 |

### artifact

成果物記録:

```
- <ts> | artifact | worktree | path=/path/to/worktree
- <ts> | artifact | branch | name=issue/xxx/run-yyy
- <ts> | artifact | pr | url=https://github.com/...
```

### test

テスト結果:

```
- <ts> | test | <test_name> | result=PASS|FAIL | log=...
```

### question

質問（人間の判断が必要）:

```
- <ts> | question | <qid> | text="..." | choices="A,B" | severity=...
```

### answer

質問への回答:

```
- <ts> | answer | <qid> | text="..." | by=user
```

### note

人間メモ:

```
- <ts> | note | <title> | text="..."
```

### monitor

daemon検出（参考情報）:

| name | 説明 |
|------|------|
| working | 出力が流れている |
| stalling | N秒以上出力なし |
| idle | アイドル状態 |

## 未回答判定

`question(qid)` が存在し、その後に `answer(qid)` が存在しない → 未回答
