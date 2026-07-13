# orch run-session lifecycle — master plan (2026-07-13)

Status: in progress — Stage 0 / Stage 1 / T2c merged(PR #531 / #532 / #533、2026-07-13)。残 = Stage 2(reaper)・Stage 3(revive)・Stage 4(観測面)。
Basis: ADR-0005 (`docs/adr/defadr_0005_run_session_lifecycle.hy`)。
2026-07-11 インシデント(terminal run のセッション 25 個が最大 5.5 日生存、
CPU 4 コア焼却、`orch repair --force` で回収不能)の根本対策。
設計議論はユーザー + frontier で 2026-07-12..13 に確定済み。

> **2026-07-13 追記(所有者裁定)**: P-1/P-2(coupling-core-roadmap.md)の
> 根治方向 — orch backend の doeff-sessionhost 化 — は合意済みだが、
> **beta 期間中は現行 backend を維持**し、差し替えは beta 後の大型
> バージョンアップとして行う。本 plan の残 stage(2〜4)は現行 backend の
> 枠内で beta 配布前に完遂する。前提工事(ADR-DOE-AGENTS-006
> conversation/resume/fork + S21 conformance)は doeff 側 main に merge 済み
> (doeff PR #517/#518/#519)。

## Source of truth

| 種別 | パス / 参照 |
|------|------------|
| 本 ADR(executable) | `docs/adr/defadr_0005_run_session_lifecycle.hy` |
| 遷移コア spec | `docs/design/run-state-machine.md`(§2 O2/O3、§5 L4、§9.7/§11.5 note-ledger 先例) |
| watchlist | `docs/design/coupling-core-roadmap.md`(WL#1/#2、B3 に cli/tick.go 言及) |
| 先行 ADR | `docs/adr/ADR-0001..0003.md`、`docs/adr/defadr_0004_store_writes_through_daemon.hy` |
| 接地調査(4 subagent、2026-07-12) | 本ファイル「Current state」表に転記済み(file:line) |
| 発端インシデント記録 | ca-pro メモリ `agent-watchdog.md`(2026-07-11、外部 watchdog は導入済み) |

## Current state(観測事実、全て file:line 裏取り済み)

| # | 事実 | 根拠 |
|---|------|------|
| F1 | step() の effect 語彙にセッション kill が無い。自律終端遷移は全てセッションを残す | `internal/daemon/step.go:281-310,454-468` |
| F2 | terminal run は monitor に二度と巡回されない(kill 再試行機会ゼロ) | `daemon.go:376-378`、`monitor.go:69` |
| F3 | repair の孤児検出は status-blind(terminal でも run が居れば expected) | `proto_handler.go:3743-3780,:3757` |
| F4 | claude/codex run に agent ネイティブ session 識別子の記録が無い(claude に --session-id 不使用、"rollout" 0 件)。opencode のみ `opencode_session` artifact で記録済み | `agent/claude.go:20-57`、`agent/codex.go:21-54`、`socket.go:2001-2058`、`model/run.go:248-250` |
| F5 | tick の "resume" イベントは消費者ゼロ(fold/monitor/step のどこも読まない)。proto ResumeRun RPC も未使用 | `cli/tick.go:160-171`、`socket.go:5723-5772`、`proto_handler.go:4006-4046` |
| F6 | kill 失敗はサイレント(warning+nil / stop は kill 失敗でも canceled 書込 / Multiplexer 空で kill スキップ)。delete はセッションを殺さない | `socket.go:5123-5132`、`proto_handler.go:1906-1911,3356-3394` |
| F7 | clean はイベント無記録で worktree 削除。restart-from(run 参照)は worktree 消失で失敗、`--branch` 経路のみ再生成可 | `proto_handler.go:3406-3422`、`socket.go:4257-4260,4206-4217` |
| F8 | reaper の置き場所: daemon 5s tick の兄弟パス(periodicFetch 型自己スロットル)。config は KnownFields(true) で 3 箇所編集必須(Config/fileConfig/merge) | `daemon.go:236-274,243-245,418-428`、`config.go:415,196-265,570-586` |
| F9 | issue status は store から読める(ListRuns enrichment に前例)。issue resolve → run 連鎖は現状無し | `proto_handler.go:1013-1046,1919` |
| F10 | kill/snapshot/worktree の実行プリミティブは全部既存(stopRunSession / worker lease StopRunPayload / mux.KillSession / CapturePane / removeRunWorktree) | `socket.go:5096`、`proto_handler.go:1883-1899,3974,405-453`、`multiplexer/tmux.go:378-380` |
| F11 | capture は一切永続化されない(artifact 語彙に capture 無し) | `cli/capture.go:63-103`、ground-record 調査 |
| F12 | terminal 脱出は source=user のみ許可(in-place revive の合法性根拠) | `model/event.go:91-96`、`store/file/file.go:955-967` |

Gap(spec と実装の乖離、ついで修正候補): repair help は "orphaned sessions and
worktrees" と言うが worktree 検出は未実装(`cli/repair.go:32`)。

## 依存図

```
ADR-0005 (law LS1-LS6)
  │
  ├─ Stage 0  tick 撤去(独立・機械的)────────────────┐
  │                                                    │
  ├─ Stage 1  identity at launch(agent_session)       │
  │      │  claude --session-id / codex cwd-match      │
  │      ▼                                             │
  ├─ Stage 2  reap(snapshot→note→kill + reconciler)   │ 全 stage:
  │      │  + O3 吸収(step/monitor、コア変更)         │ red test 先行
  │      ▼                                             │ (invariant TDD)
  ├─ Stage 3  revive(send/attach in-place)            │
  │      │  + delete kill / clean worktree_removed     │
  │      ▼                                             │
  └─ Stage 4  観測面(ps/show/debug 列、capture 提供、query 列)
```

## Completion gates(全体完成条件)

1. LS1: 全バックエンドで「grace 経過後、terminal/resolved/TTL 超過 run の
   `run-*` セッションが生存しない」— reconciler property test + 実機 e2e。
2. LS2/LS5: reap は agent_session + worktree 存在の記録事実がある run のみ。
   revive 不能 run のセッションは kept+reported(repair 出力に列挙)。
3. LS3: reap 済み世代への session-gone が dead-check を進めない
   (step property test; 偽 failed の counterexample が red→green)。
4. LS4: 観測系動詞が boot しないことの test。capture は snapshot を返す。
5. claude/codex run で `orch send` による reaped→revived→応答の e2e が通る
   (`docs/development/e2e-backend-matrix.md` の実 agent マトリクスに従う)。
6. tick が CLI/docs/proto から消え、semgrep `adr0005-tick-stays-dead` が
   再導入を落とす。`make lint` green、`uv run pytest docs/adr -q` green。
7. 2026-07-11 インシデント再現シナリオ(terminal 化→放置)で 10 分以内に
   セッション回収、`orch send` で復活、を実機確認。

## Master TODO

| ID | source | 作業 | red counterexample | green 機構 | status |
|----|--------|------|--------------------|-----------|--------|
| T0a | R7/F5 | cli/tick.go + tick_test.go + root.go:180 登録 + integration test + docs(commands/statuses/roadmap/plugin reference)撤去。proto ResumeRun RPC 撤去 | docs に `orch tick` が残る(semgrep bad fixture) | 削除 + `adr0005-tick-stays-dead` | **done**(PR #531) |
| T1a | R1 | claude: launch で UUID 発番、`--session-id` 付与(agent/claude.go)+ `agent_session` artifact append(launch ladder、error-artifact 先例に倣う) | 起動後の run record に claude session id が無い(test: DeriveState fold) | LaunchConfig 拡張 + artifact append + fold(model/run.go、opencode_session 同型) | **done**(PR #532) |
| T1b | R1 | codex: boot 後 `$CODEX_HOME/sessions/**/rollout-*.jsonl` 先頭行 `payload.cwd == worktree_path` で id 解決 → 同 artifact | codex run に識別子無し | boot 後 resolver(リトライ付き、見つからなければ error artifact = unreapable) | **done**(PR #532) |
| T1c | R1 | model.Run/orchapi.Run/RunSnapshot/query DDL+loader+view に AgentSessionID 面出し | query で引けない | F4 の opencode_session 配線の複製 | **done**(PR #532、file store run index cache の拡張 + version bump 4→5 込み) |
| T2a | R2 | reaper reconciler: daemon tick 兄弟パス + `reaper:` config(terminal_grace=10m, resolved_grace=1h, idle_ttl=168h)+ 全 status ListRuns + issue status 参照 | 2026-07-11 再現: terminal run のセッションが grace 後も生存 | 新パス(status 書込なし、note/artifact のみ) | todo |
| T2b | R3/F11 | reap protocol: CapturePane→sidecar file + `session_snapshot` artifact → `session_reaped` note → KillSession。失敗= error artifact+retry | kill 前に note が無い/失敗が warning | 実装順序を test で固定 | todo |
| T2c | LS3 | gatherer が session_reaped/revived note を fold→観測に注入、step() O3 腕が reap 済み世代の gone を吸収(dead-check 不進行)。**コア変更: frontier+human** | reap→3 gone→偽 failed | run-state-machine.md §改訂同梱 + step property test | **done**(PR #533、§12 L-S3) |
| T2d | F3 | findOrphanedSessions を status-aware に(terminal run のセッション報告)+ repair help の worktree 虚偽記載修正 | 25 個が repair で報告されない | expected 集合に status 条件 | todo |
| T3a | R5 | send/attach auto-revive: reaped 判定→前提検証(stored facts)→同名セッション再 boot(claude --resume chain-tip / codex resume)→ `session_revived` note + 新世代 agent_session → 送信続行。terminal は source=user status event で再入 | reaped run へ send がエラー/黙って新規文脈 | daemon 側 revive 経路(launch ladder 変種) | todo |
| T3b | R8 | delete が kill(R3 経由)してから record 削除。clean が `worktree_removed` note 記録。restart-from(run 参照)の worktree 消失エラーに `--branch` 導線明記 | delete 後セッション残存 / clean 後 revive が黙って fresh | 各 handler 修正 | todo |
| T3c | F6 | kill 失敗 fail-fast 化(socket.go:5126 warning 撲滅、Multiplexer 空=error)+ `adr0005-no-warning-only-kill-failure` semgrep live 化 | 既存 bad fixture | エラー伝播 + semgrep | todo |
| T4a | R6 | ps/show/debug に session 状態列(live/reaped(revivable)/reaped(unrevivable))。capture は snapshot+reaped 通知を返す。wait は reaped=帰着扱い | reaped run が ALIVE - としか見えない | daemon 計算(client 側導出禁止の既存境界に従う) | todo |
| T4b | R6 | query runs テーブルに agent_session_id / session_state 列 | SQL で引けない | schema.go+loader.go+view | todo |

## Staged plan

- **Stage 0(T0a)**: tick 撤去。他 stage と独立、先行可。
- **Stage 1(T1a-c)**: identity at launch。red: 「launch 直後の run に
  agent_session が fold されている」test を先に落とす。
- **Stage 2(T2a-d)**: reap。red: インシデント再現(terminal+生存セッション)
  と偽 failed counterexample。T2c のみコア面(step.go/monitor.go)=
  frontier+human、spec 改訂(run-state-machine.md に LS3 吸収と O3 改訂を
  追記)を同一 change set で。
- **Stage 3(T3a-c)**: revive + 動詞整合。red: reaped run への send e2e。
- **Stage 4(T4a-b)**: 観測面。red: ps JSON に session_state が無い。
- 各 stage 完了時に `make lint` + `go test ./...` + 関連 e2e
  (`docs/development/e2e-backend-matrix.md` の実 agent マトリクス)。

## Subagent spawn strategy

| role | task | scope | model | 並列 | 検証 |
|------|------|-------|-------|------|------|
| architect(親) | ADR/law/spec 改訂、T2c コア変更、全 diff レビュー | docs/design、step.go、monitor.go | frontier(本セッション) | — | property tests |
| worker-mech-1 | T0a tick 撤去一式 | cli/tick*、root.go、docs、proto | sonnet 可(機械的) | Stage0 | build+lint+grep |
| worker-mech-2 | T1c/T4b 面出し配線(fold→model→proto→query) | model/orchapi/query | sonnet 可(opencode_session の複製) | Stage1/4 | go test |
| worker-core | T1a/T1b/T2a/T2b/T3a 実装 | daemon/agent | frontier(orch run --agent claude fable xhigh 委譲可 — 決定は ADR で閉鎖済み) | 順次 | red→green |
| reviewer | 各 PR の adversarial review(tripwire checklist) | read-only | codex(cx read-only) | 各 stage 後 | verdict |

実装は orch 自身の issue/run で回す(dogfood)。コア面 T2c だけは
orch 委譲せず親セッション(frontier+human)で行う。

## Non-goals

- gemini / opencode の revive 実装(gemini は識別子なし= R4 により reap
  対象外で安全側に倒れる。opencode はセッションが HTTP 永続で mux reap の
  意味が異なる — 別 ADR)。
- worktree GC ポリシー変更(clean のイベント記録以外)。7d TTL による
  worktree 削除はしない — reap はセッションのみ。
- RunEventFrame proto(status 遷移 bus)への reap/revive イベント追加
  (note イベントが durable record。bus 拡張は需要が出たら)。
- 外部 watchdog(`~/.local/bin/agent-watchdog`)の置換 — 非 orch エージェント
  用として残る。

## Immediate next action

(2026-07-13 更新)Stage 0 / Stage 1 / T2c は merge 済み。次は Stage 2:
issue `adr0005-s2-session-reaper`(verified issue、前提 PR #533 merge 済み)を
orch run で dispatch。merge 後 Stage 3(T3a はコア隣接 = frontier + 人間、
T3b/T3c は委譲)→ Stage 4。beta 配布は Stage 4 完了後。
