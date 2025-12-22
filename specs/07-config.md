# Configuration

## 解決順序

orch は以下の順序で設定を解決する:

1. コマンドラインオプション（`--vault`, `--backend` 等）
2. カレントディレクトリの `.orch/config.yaml`
3. 親ディレクトリの `.orch/config.yaml`（上方向に探索）
4. 環境変数（`ORCH_VAULT`, `ORCH_BACKEND` 等）
5. グローバル設定（`~/.config/orch/config.yaml`）

## リポジトリローカル設定

リポジトリのルートに `.orch/` ディレクトリを作成し、設定を保存:

```
repo/
  .orch/
    config.yaml     # リポジトリ固有の設定
  .git/
  src/
  ...
```

### .orch/config.yaml

```yaml
# vault path (relative to repo root or absolute)
vault: ~/vault

# または同じvault内にissueを置く場合
vault: .

# default agent
agent: claude

# worktree settings
worktree_root: .git-worktrees

# base branch for new runs
base_branch: main

# default PR target branch
pr_target_branch: main
```

### 自動検出

`orch` コマンド実行時:

1. カレントディレクトリから上方向に `.orch/config.yaml` を探索
2. 見つかった設定を親→子の順で読み込み（近い設定ほど優先）
3. 環境変数はそれより低い優先度で利用
4. 見つからなければグローバル設定にフォールバック

これにより、ユーザーはリポジトリ内で単に `orch ps` や `orch run ISSUE` を実行できる。

## グローバル設定

`~/.config/orch/config.yaml`:

```yaml
# default vault for all repos without local config
default_vault: ~/vault

# default agent
agent: claude

# log level
log_level: info
```

## 環境変数

| 変数 | 説明 |
|------|------|
| `ORCH_VAULT` | Vault path |
| `ORCH_BACKEND` | Backend type (file/github/linear) |
| `ORCH_AGENT` | Default agent |
| `ORCH_LOG_LEVEL` | Log level |
| `ORCH_PR_TARGET_BRANCH` | Default PR target branch |
