# orch ローカルクイックスタート(クラスタ設定なし)

このガイドは 1 台のマシンだけで orch を試すための最短経路です — ローカル
daemon、ローカル worker、ローカルの agent セッションのみを使い、リモート
master・`client.yaml`・SSH は一切登場しません。掲載している手順はすべて
orch `v1.4.0-beta` 相当のバイナリで実際に実行・検証済みです。

英語の全体ガイドは [Getting Started](./getting-started.md) を参照してください。

## 1. 前提ツール

| ツール | 用途 | 確認コマンド |
|--------|------|--------------|
| `git` | worktree 管理 | `git --version` |
| `tmux` | agent セッションの実行環境 | `tmux -V` |
| agent CLI(いずれか 1 つ、**ログイン済み**) | 実際に作業する主体 | `codex --version` / `claude --version` |
| `gh`(任意) | PR ワークフローを試す場合のみ | `gh auth status` |

agent CLI(`codex` / `claude`)には orch を使う前に一度対話ログインして
ください。orch はその認証状態をそのまま使います — API キーを orch や
エージェントに渡す必要はありません(渡さないでください)。

orch 本体のインストール:

```bash
curl -sSL https://raw.githubusercontent.com/proboscis/orch/main/install.sh | bash
orch --help   # 動作確認
```

## 2. サンドボックスプロジェクトの準備(初回のみ、約 2 分)

最初の run は実プロジェクトではなく使い捨ての repo で試すことを勧めます。

```bash
mkdir ~/orch-sandbox && cd ~/orch-sandbox
git init -b main
echo "# orch sandbox" > README.md

# orch はプロジェクトの同一性(project identity)を origin URL から導出します。
# 基本ループを試すだけなら GitHub 上に実在しなくても構いません
# (URL は識別子として使われるだけ)。PR ワークフローまで試すなら
# 実在する自分の repo URL を使ってください。
git remote add origin https://github.com/<you>/orch-sandbox.git

mkdir .orch
cat > .orch/config.yaml << 'EOF'
agent: codex          # または claude
base_branch: main
issues:
  backend: local
  path: ./issues      # issue の markdown を repo 内に置く
EOF

git add -A && git commit -m "init orch sandbox"
```

このプロジェクトを daemon に登録します(identity → checkout の対応付け):

```bash
orch daemon repo register "$(pwd)"
# => Registered repo mapping: <you>-orch-sandbox -> /Users/<you>/orch-sandbox
```

daemon の再起動は不要です。登録内容は
`~/.config/orch/projects/<project_id>.yaml` に保存されます。

> **補足**: issue ファイルは `path` 直下ではなく `<path>/issues/`
> (`Issues/` ディレクトリが既にあればそちら)に作られます。

## 3. ローカル worker の起動(必須)

daemon は最初の orch コマンドで自動起動しますが、**agent セッションを
実際に起動する worker は自動では立ち上がりません**。worker なしで
`orch run` すると `no active workers available` で失敗します。

```bash
orch worker start
orch worker status   # Local Process: running / Master Registration: active を確認
```

worker はマシンの再起動後には残りません。突然 run が
`no active workers available` で失敗するようになったら
`orch worker start` を再実行してください。

## 4. ゴールデンパス

orch のコマンドはカレントディレクトリからプロジェクトを解決します —
**サンドボックス repo の中で**実行してください。

```bash
cd ~/orch-sandbox

# 1. タスクを issue として記述する
orch issue create hello-world --title "Add a hello world script" << 'EOF'
Create hello.py at the repository root that prints "Hello, World!" when run
with python3. Keep it to a few lines. Do not create anything else.
EOF

# 2. agent を起動する(--no-pr: まずは PR なしで)
orch run hello-world --no-pr

# 3. 観察する
orch ps                      # booting → running → waiting と遷移する
orch capture hello-world     # agent のターミナル画面のスナップショット
```

### 初回 run では agent が trust 確認を出す

まっさらなマシン/ディレクトリでは、agent CLI が初回に対話ゲート
(「Do you trust the contents of this directory?」など)を表示し、run は
`waiting` で停止します。これは故障ではなく、orch の対話ループそのものです:

```bash
orch capture hello-world     # 何を聞かれているか確認する
orch send hello-world ""     # 空メッセージ = Enter 送信(デフォルト選択を受理)
# あるいはターミナルを直接操作する:
orch attach hello-world      # 対話後、Ctrl+B D でデタッチ
```

### 完了を待って結果を確認する

```bash
orch wait hello-world --timeout 600
```

`waiting` は「agent の入力欄が空いている」ことを意味します — trust ゲート
通過後の `waiting` は通常「agent がそのターンを終えた」合図です。
**結論を出す前に必ず `orch capture` で agent の報告を読んでください。**
成果物は隔離された worktree にあります(パスは `orch ps` /
`orch show hello-world` で表示されます):

```bash
python3 ~/.orch/worktrees/hello-world/<run-dir>/hello.py   # → Hello, World!
```

### 片付け

```bash
orch stop hello-world      # セッションを終了する(状態は canceled になる)
orch resolve hello-world   # issue を完了扱いにする
```

実在する GitHub repo と認証済みの `gh` があれば、`--no-pr` を外すだけで
agent は commit → push → PR 作成まで行い、run は `pr_open` であなたの
レビューを待ちます。これが本来の日常ワークフローです。

## 5. 遭遇しうるエラー(すべて意図的に再現して確認済み)

| 症状 | 原因 | 対処 |
|------|------|------|
| `project identity required: failed to resolve git remote` | origin remote が無い、または repo の外で実行 | `git remote add origin …`、repo 内に `cd`(または `ORCH_PROJECT` を設定) |
| `unknown project_id "…" (register daemon project mapping)` | 手順 2 の登録漏れ | `orch daemon repo register "$(pwd)"` |
| `no active workers available` | worker 未起動(再起動後を含む) | `orch worker start` |
| `capture`/`send` が `run not found` | 別プロジェクトのディレクトリに居る(CWD でプロジェクト解決) | サンドボックス repo に `cd` してから実行 |
| 起動直後から `waiting` のまま | agent の trust/onboarding ダイアログ | `orch capture` で確認 → `orch send <run> ""` か `orch attach` |
| すべての issue コマンドが `daemon error: store_error` | 壊れた issue ファイルが 1 つでもあると store 全体が fail-loud | frontmatter の `status` は `open`/`closed`/`resolved` のみ。daemon ログ(macOS: `~/Library/Logs/orch/daemon.log`、Linux: `~/.local/state/orch/daemon.log`)が対象ファイル名を報告する |

## 6. 今回あえて使わなかったもの

`client.yaml`、`ORCH_REMOTE`、`config.targets`、`--on <target>`、SSH —
これらはすべてマルチホスト(クラスタ)運用のための構成要素で、ローカル
テストには不要です。クラスタ構成は [Remote Usage](./remote-usage.md) を
参照してください。
