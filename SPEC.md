
rch CLI Spec v0.2

0. 目的

orch は「複数LLM CLI（claude/codex/gemini等）を、issue/run/event という統一語彙で運用する」ためのオーケストレーター。
中核は non-interactive（対話しない）。対話が必要な局面はイベント（question）として外部化し、answer と tick で再開する。

1. 設計原則
	1.	non-interactive がデフォルト

	•	実行中に入力待ちはしない
	•	人間の判断が必要なら question event を追記して終了（blocked）

	2.	真実は append-only events

	•	既存イベントを書き換えない
	•	状態（status/phase等）はイベントから派生。frontmatterはキャッシュ可だが必須ではない

	3.	UIはskin

	•	VSCode/fzf/tmuxは後付け
	•	CLIは安定契約（互換性）として扱う

	4.	PTY/TTY を自前で握らない

	•	対話は tmux attach に委譲（画像コピペ等のため必須）
	•	orch 自身は tmux new-session / send-keys / capture-pane を必要最小限のみ使用（後者は任意）

⸻

2. 用語

Issue: 仕様の単位。IDで一意（例: plc124）
Run: 実行の単位。同一Issueから複数Runが発生する
Event: Runに追記される1行レコード（append-only）
RUN_REF: ISSUE_ID#RUN_ID 形式（または ISSUE_ID だけ＝最新run）

⸻

3. Knowledge Store（バックエンド）抽象

orch は「知識ベース」を抽象化して扱う。最初の実装は file backend（Obsidian vault）。

3.1 必須操作（概念）
	•	ResolveIssue(issue_id) -> IssueDoc（タイトル/本文/Frontmatter）
	•	CreateRun(issue_id, run_id, metadata) -> RunDoc（パス含む）
	•	AppendEvent(run_ref, event_line) -> void
	•	ListRuns(filter) -> []RunSummary（status/phase/updated等は派生またはキャッシュ）
	•	GetRun(run_ref) -> RunDoc（events tail含む）

3.2 File backend（推奨ディレクトリ構造）

vault/
issues/<ISSUE_ID>.md
runs/<ISSUE_ID>/<RUN_ID>.md
runs/<ISSUE_ID>/<RUN_ID>.log/   （任意：ログ格納用）

※ ObsidianはUI。vaultはただのファイル集合。

⸻

4. 主要コマンド（v0.2）

共通オプション（全コマンド）
	•	–vault PATH（または env ORCH_VAULT）
	•	–backend file|github|linear（v0.2では file を正式、他は予約）
	•	–json（機械可読JSON出力）
	•	–tsv（fzf向け。ps等で有効）
	•	–quiet（人間向け出力を抑制）
	•	–log-level error|warn|info|debug

4.1 orch run ISSUE_ID

目的: 新しいrunを作成し、worktreeを作成し、agentを起動する（即return）

オプション
	•	–new（常に新run。デフォルト）
	•	–reuse（最新runを再開。blocked向け）
	•	–run-id <RUN_ID>（手動指定）
	•	–agent claude|codex|gemini|custom:
	•	–agent-cmd （custom時の起動コマンド）
	•	–base-branch main（デフォルトmain）
	•	–branch （省略時は規約生成）
	•	–worktree-root （例: .git-worktrees）
	•	–repo-root （git rootを明示。省略時は探索）
	•	–tmux / –no-tmux（デフォルトtmux）
	•	–tmux-session （省略時は規約生成）
	•	–dry-run（副作用なし：作成予定を表示）

規約（デフォルト）
	•	RUN_ID = YYYYMMDD-HHMMSS
	•	branch = issue/<ISSUE_ID>/run-<RUN_ID>
	•	worktree_path = <worktree_root>/<ISSUE_ID>/<RUN_ID>
	•	tmux_session = run-<ISSUE_ID>-<RUN_ID>

副作用（–dry-run除く）
	•	Run doc作成
	•	Event追記: status=queued/booting/running, artifact(worktree/branch/session) 等
	•	git worktree add + checkout
	•	tmux new-session で agent起動（非対話モード）

終了コード
	•	0 成功（起動まで成功。後続の失敗はeventsで表現）
	•	2 issue not found
	•	3 worktree error
	•	4 tmux error
	•	5 agent launch error
	•	10 internal error

JSON出力（例）
ok, issue_id, run_id, run_path, branch, worktree_path, tmux_session, status

⸻

4.2 orch ps

目的: runs一覧を表示（人間/機械）

オプション
	•	–status running,blocked,failed,pr_open,done
	•	–issue <ISSUE_ID>
	•	–limit N（default 50）
	•	–sort updated|started（default updated）
	•	–since 

出力
	•	通常: 人間向け表
	•	–tsv: fzf用固定列（下記）
	•	–json: items配列

TSV列（固定順）
issue_id, run_id, status, phase, updated_at, pr_url, branch, worktree_path, tmux_session

終了コード
	•	0 成功
	•	10 internal error

⸻

4.3 orch show RUN_REF

目的: 1runの詳細（events tail、未回答question、主要artifact）

オプション
	•	–tail N（default 80）
	•	–questions（未回答のみ）
	•	–events-only（イベントだけ）

終了コード
	•	0 成功
	•	6 run not found
	•	10 internal error

⸻

4.4 orch attach RUN_REF

目的: tmux attach（画像コピペ等の手動対話）

オプション
	•	–pane log|shell（予約。v0.2ではセッションattachのみでもOK）
	•	–window （任意）
	•	–create（セッションが無い時に作る。デフォルトoff）

終了コード
	•	0 attach成功
	•	6 session not found
	•	10 internal error

⸻

4.5 orch answer RUN_REF QUESTION_ID

目的: questionに回答イベントを追記（non-interactive）

オプション
	•	–text “…” または –file 
	•	–by user|system（default user）

副作用
	•	Event追記: answer |  | text=…

終了コード
	•	0 成功
	•	6 run not found
	•	7 question not found（判定できない場合）
	•	10 internal error

⸻

4.6 orch tick RUN_REF | –all

目的: blocked等のrunを再開するトリガ（質問が解消されていれば次フェーズを進める）

オプション
	•	–only-blocked（default on when –all）
	•	–agent …（再開時のagent指定）
	•	–max N（–all時の最大処理件数）

挙動（標準）
	•	runのeventsを読み、未回答questionが無ければ agent を再起動（新window推奨）
	•	未回答があれば何もしない（0で返して良い、または状態をjsonで返す）

終了コード
	•	0 成功（対象なし含む）
	•	10 internal error

⸻

4.7 orch open ISSUE_ID|RUN_REF

目的: Obsidian/Editorで該当ノートを開く（利便）

オプション
	•	–app obsidian|editor|default
	•	–print-path（開かずにパスだけ返す）

終了コード
	•	0 成功
	•	10 internal error

⸻

5. Agent adapter（差し替え層）仕様

orch は “LLMの状態”を直接取らない。agentは単なる外部プロセスとして扱う。

v0.2での最小責務
	•	起動コマンドに以下を環境変数で渡せること
	•	ORCH_ISSUE_ID
	•	ORCH_RUN_ID
	•	ORCH_RUN_PATH
	•	ORCH_WORKTREE_PATH
	•	ORCH_BRANCH
	•	ORCH_VAULT
	•	agent は進捗/状態を events に追記してよい（推奨は orch subcommand 経由）
	•	推奨: agentは orch event append ... を呼ぶ（v0.3で追加予定）
	•	v0.2: agentが直接run doc末尾へ追記してもよいが、競合回避のため「append-only」「短い1行」厳守

⸻

6. Eventフォーマット（Dataview friendly）

Run本文に箇条書きで追記する。1行 = 1イベント。

形式
	•	 |  |  | key=value | key=value …

ts: ISO8601（例: 2025-12-20T11:45:10+09:00）

推奨type/name
	•	status | queued/booting/running/blocked/pr_open/done/failed/canceled
	•	phase | plan/implement/test/pr/review
	•	artifact | worktree path=… / branch name=… / pr url=…
	•	test |  result=PASS|FAIL log=…
	•	question |  text=”…” choices=“A,B” severity=…
	•	answer |  text=”…” by=user
	•	note |  text=”…”（人間メモ）

未回答判定（v0.2標準）
	•	question(qid) が存在し、その後に answer(qid) が存在しない → 未回答

⸻

7. fzf対応（次ステージのための今の約束）
	•	orch ps --tsv の列順固定（上記）
	•	RUN_REF = ISSUE_ID#RUN_ID の正規形固定
	•	orch show/attach/answer/tick はすべて RUN_REF を受け取れる

これだけ守れば orch ui（fzfラッパー）は後から薄く足せる。

⸻

8. 互換性ポリシー
	•	v0.x: 破壊的変更あり得るが、以下は極力維持
	•	サブコマンド名
	•	--json のトップレベルキー（ok/issue_id/run_id等）
	•	TSV列順
	•	v1.0: RUN_REF、イベント形式、主要サブコマンドは固定

⸻

9. 最低限の実装順序（推奨）
	1.	ps（runs走査・イベントtail解析）
	2.	run（worktree+tmux起動+events追記）
	3.	attach
	4.	answer
	5.	tick
	6.	show / open

