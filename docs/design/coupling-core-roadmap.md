# Coupling-Core Roadmap — orch

Status: living document
Date: 2026-06-12
Basis: /coupling-core 診断 (2026-06-12)。`docs/design/run-state-machine.md`
(step() v1, PR #460) を「結晶化完了扱い・S4 委譲開放」とする所有者判断を
前提に、S4 を安全にするための残作業を全フェーズ列挙する。
このファイルは watchlist を兼ねる(skill の生きた成果物)。

## 現状サマリ

- step() v1 マージ済み。law L1–L6 は `internal/daemon/step_test.go` の
  9 本の property test で機械検証済み。
- **未閉鎖**: status イベント書き込み面が step 実行器の外に 15+ 箇所
  (socket.go ×10, proto_handler.go ×4, cli/tick.go [ADR-0005 Stage 0 で撤去済み],
  cli/repair.go)。
  旧 Go TUI の 2 writer は TUI 廃止と同時に撤去済み。
- proto_handler.go の 4 箇所が `_ = st.AppendEvent(...)` でエラーを黙殺。
- I2(再起動透過性)・I5–I8 は doc 自身が違反と明記。
- worker lease は churn(PR #457/#461, c5aba240)があるのに ADR/law 無し。
- CI: `.github/workflows/quality.yaml` が `make lint`(semgrep)を実行済み
  → `.semgrep/` に足したルールはそのまま delegate PR のゲートになる。

## フェーズ依存関係

```
A 柵を立てる ──────────────► S4 が安全になる(最優先・即日)
│
├─► B step() v2 統合 ──► C I2/fold + 不変条件完済 ──► D シミュレータ
│        (書き込み面→1)        (状態=イベントの畳み込み)     (完全性チェック)
│
├─► E worker lease 結晶化(並行・独立)
│
└─► F S4 運用レジーム(A と同日に開始、以後継続)
```

---

## Phase A — 委譲の柵(最優先、~1 日箱)

S4 入場条件「watchlist 全項目が 4 層固定 or 明示隔離」の最小充足。
コア委譲が既に進行中なので、これだけは他の何よりも先。

### A1. status 書き込み面の semgrep 閉鎖【frontier、2–4h】
- 成果物: `.semgrep/run-status-write-surface.yaml`
- 内容: step 実行器(`internal/daemon/monitor.go` updateStatus)以外での
  `model.NewStatusEvent` / `Type: "status"` の AppendEvent を禁止。
  既存 15+ 箇所は明示 whitelist(`paths` 除外 or `nosemgrep` 注記)で
  「凍結された負債」として列挙 — 新規追加だけが落ちる。
- exit: 16 箇所目の writer を足した PR が `make lint` で落ちる。

### A2. AppendEvent エラー黙殺の根絶【決定=frontier / 実装=codex】
- 対象: `proto_handler.go:1513,1564,1606,1657` の `_ = st.AppendEvent(...)`
- 決定事項(frontier): 失敗時の伝播先 — リクエスト失敗にするか、
  error artifact + ログにするか。fail-fast 原則に従い前者を既定とする。
- 成果物: 4 箇所の修正 + `.semgrep/` ルール `no-ignored-append-event`
  (`_ = $ST.AppendEvent(...)` パターン禁止)。
- exit: エラー黙殺が lint で再発不能。

### A3. routing rule の明文化【frontier、1h】
- 成果物: `AGENTS.md` に追記 — コア面
  (`internal/daemon/{step,monitor,socket,proto_handler}.go`、lease 系)
  を触る issue は frontier + human、それ以外は codex 委譲可。
  delegate PR レビュー時のトリップワイヤ(新 ID 型・registry・共有フラグ・
  状態ミラー・手動同期コメント)を checklist 化。

---

## Phase B — step() v2: 書き込み面を 1 に縮める(2–3 日箱)

run-state-machine.md が「v2 候補」と明記した W2–W10 の統合。
A1 の whitelist がフェーズの進捗メーター(縮んで空になったら完了)。

### B1. launch ladder W2–W5 → step 統合【frontier】
- 実装済み(2026-06-13, run-state-machine.md §7 D-B1): W2–W5/W7 の全 37
  append サイトを O8 観測(`launchSignal`)化。遷移決定は純粋な
  `stepLaunchProgress`、実行は `reportLaunchProgress`(worker プロセスでも
  動く launch 面実行器)。updateStatus の guard/append 核は
  `commitRunStatus`(status_commit.go)へ抽出され、status イベント構築点は
  文字通り 1 箇所になった。失敗 verdict は `launch_<step>` reason を獲得
  (L9)。whitelist 47 → 10。
- proto_handler.go の 4 サイトは §6 の W9 disposition(quarantine — law
  boundary)に従い対象外(本節の旧記述は disposition 決定前のもの)。
- 残 v2: 4 重 imperative bootstrap の制御フロー自体の統合。

### B2. W6 feedback / W8 stop / W9 master projection / W10 external append【frontier 判断 → codex 実装可】
- 各サイトを「step の観測に統合する」か「明示隔離(理由を
  run-state-machine.md に追記)」のどちらかに決定する。
  どちらでもよいが、**未決定のまま残すことだけが禁止**。
- W10(external append、`socket.go handleAppendEvent`)は外界橋
  (law 境界)として隔離が妥当な候補 — `CanTransitionStatus` が唯一の守り
  であることを明記。

### B3. クライアント面からの status 書き込み撤去【API 設計=frontier / 移植=codex】
- 対象: `cli/tick.go:163` (ADR-0005 Stage 0 で撤去済み)、
  `cli/repair.go:207`。旧 Go TUI の
  canceled/done writer は TUI 廃止と同時に撤去済み。
- クライアントが status イベントを組み立てるのをやめ、意味のある
  daemon API 動詞(CancelRun / ResolveRun / Repair)に置き換える。
  「状態計算は daemon」の境界の書き込み版。
- 成果物: API 動詞 + `.semgrep/` ルール `client-no-status-append`
  (internal/cli での Type:"status" append 禁止)。
- exit: status イベントの append 箇所が正確に 1(step 実行器)。

---

## Phase C — 再起動透過性(I2)+ 不変条件の完済(2–3 日箱)

### C1. RunState = fold(event log)【frontier — store-of-record vs derived の核】
- 意味カウンタ(WasAlive / DeadCheckCount / PromptStreak / PRRecorded /
  OutputHash)を daemon 再起動で失わない構造に。選択肢を式で閉じる:
  (a) 起動時に event log を replay して導出、
  (b) カウンタ変化をイベントとして永続化、
  (c) 周期スナップショット + replay。
- 新 law(再起動透過性): 任意の時点で daemon を再起動しても、
  同じ観測列に対する step の遷移列は不変。property test 化。
- exit: I2 が doc の表で "holds" になり、test が守る。

### C2. I6 — queued 孤児の監視【codex(verified issue)】
- `monitorAll` が `queued` を列挙しないため、起動途中の daemon クラッシュで
  孤児化した run が二度と観測されない問題。queued を監視対象に含め、
  「すべての非 terminal run はいつか観測される」を test 化。

### C3. I7 — local/remote verdict 非対称の解消【law 改定=frontier】
- never-alive run への verdict が remote は grace 後 `unknown`、
  local は永遠に沈黙という非対称(L3 補項で pinned)を、単一意味論に統一
  (推奨: local も grace 後 `unknown`)。L3 を改定し test 更新。

### C4. I8 — fireStatusChange の全遷移発火【codex】
- 実装済み(2026-06-12, inv-fire-status-change-all): 通知発火を
  O4 固有効果から W1 commit point (`Daemon.updateStatus`) に移し、
  append 成功後に一回だけ listener が発火することを test 化。
  PR close / dead verdict / agent inference / duplicate no-op / append 失敗を
  検証対象に含める。
- W6 feedback resume は v2 統合までの橋渡しとして legacy append 成功後に
  同じ listener fanout を呼ぶ。W2–W5/W8 は Phase B/v2 の書き込み面統合で
  W1/O7/O8 に吸収する。

### C5. I5 — PR artifact dedup の永続化【C1 から従属】
- `PRRecorded` を event log からの導出に置き換え(C1 の系)。
  再起動後の重複 `pr` artifact が出ないことを test 化。

- Phase C exit: run-state-machine.md §4 の I1–I8 が全行 "holds"。

---

## Phase D — 決定論シミュレータ + 完全性チェック(1–2 日箱)

### D1. 観測スクリプト replay ハーネス【frontier 設計 / codex 拡充】
- step + fake 効果実行器 + fake store で観測列を流す決定論シミュレータ。
- 過去の実障害(5,830 重複イベント、PR #458 の推論バグ、queued 孤児)を
  観測スクリプトとして再現・回帰固定。
- exit(skill の完全性チェック): 外界橋ハンドラの差し替え**だけ**で
  シミュレータが組めること。組めない箇所 = コアが葉に漏れている箇所。

### D2. 代数カバレッジ検査【転記=codex / 判定=frontier】
- 過去の run ライフサイクル系バグ・issue ~20 件を観測語彙
  (O1–O8)で書き直す。70% 未満しか表現できなければ観測タクソノミを
  見直す(分解の妥当性検査)。

---

## Phase E — worker lease 核の結晶化(並行可、2–3 日箱)

第 2 の核。痛点申告(lease / remote)+ churn(PR #457 snapshot SSOT、
#461 lease fail-fast、c5aba240 race 修正)があるのに ADR/law が無い。
run-state-machine.md と同じ playbook を適用する。

### E1. 調査ドキュメント【frontier】
- `docs/design/worker-lease.md`: lease の書き込み面の全列挙、
  所有権/ライフサイクル(誰が取得・更新・解放するか、master/worker
  再起動時の挙動)、heartbeat 意味論 — run-state-machine.md §1–4 の鏡。

### E2. laws + property tests【frontier】
- 候補: lease 排他性(同時 active holder ≤ 1)、heartbeat 単調性、
  failover 収束、lease 喪失 × run status の相互作用
  (lease が消えた run はどの遷移を受けるか — run 状態機械との結合点)。

### E3. 静的ガード【frontier 定義 → 以後 CI】
- lease 変異面の semgrep ルール(A1 と同型)。

---

## Phase F — S4 運用レジーム(A と同日開始、以後継続)

- F1. このファイルを watchlist として維持(下表)。新しい churn クラスタ
  が出たら行を足す。大規模 rework が起きたら postmortem → 全層更新。
- F2. choice-space gate: 核近傍の issue は、選択肢を式として列挙し
  spec の穴を埋めてからでないと codex に出さない
  (delegate は開いた分類空間を最安の答えで埋める)。
- F3. メンテループ: frontier のレビュー指摘が ~3 回繰り返されたら
  semgrep ルール/規約に焼き込む。

## Watchlist(生きた表 — 状態が変わったら更新)

| # | core | 4層の状態 | 守り | 閉鎖フェーズ |
|---|------|----------|------|------------|
| 1 | run status 遷移 | ADR✓ law✓(tested) runtime△ **static✓ (A1, 2026-06-12)** | `run-status-write-surface` ルール + 凍結 whitelist | **B1 済(47→10, 2026-06-13)**; 残は v2(W6/W8)+ 法境界(W9/W10) |
| 2 | RunState vs event log(導出状態) | **D-C1 実装済み**: 導出可能フィールドは `initialRunCore` で fold (`internal/daemon/step.go:86`)、収束カウンタは ephemeral-by-law (L7)。**static✓ (2026-07-11)** | 振る舞い: L7 `TestInitialRunCoreDerivation` / `TestInitialRunCoreRestoresLivenessAcrossRestart`(`internal/daemon/step_test.go`)。静的: `.semgrep/derived-state-guard`(`run-event-vocabulary-closed` / `no-monitor-counter-in-event` / `run-core-hydration-surface`)+ golden 分類テスト `TestRunCoreFieldsAreClassified` / `TestRunStateFieldsAreSchedulingOnly`(`internal/daemon/derived_state_guard_test.go`) | C1 完了(PR #467)+ 静的ガード済(2026-07-11) |
| 3 | worker lease 所有権/ライフサイクル | E1 doc + LL1–LL5 law 検証 + E3 static guard 実装済み | `worker-lease-mutation-surface` (`.semgrep/worker-lease-mutation/worker_lease_mutation.yaml:2`) + `TestLeaseLawCompletionFinality` (`internal/daemon/worker_plane_law_test.go:45`) / `TestLeaseLawDispatchExclusiveUntilExpiry` (`internal/daemon/worker_plane_law_test.go:73`) / `TestLeaseLawNoDispatchAfterCompletion` (`internal/daemon/worker_plane_law_test.go:100`) / `TestLeaseLawRestartAmnesiaFailsFast` (`internal/daemon/worker_plane_law_test.go:125`) / `TestLeaseLawRandomWalkInvariants` (`internal/daemon/worker_plane_law_test.go:141`) + LL1 `TestWaitForWorkerLeaseCompletionFailsFastWhenWorkerLost` (`internal/daemon/worker_plane_test.go:328`) / LL5 `TestExecuteLeaseEffectStopRunUsesSnapshotWithEmptyWorkerRunStore` (`internal/daemon/worker_plane_test.go:200`) | E1/E3 完了(PR #465/#468); E2 完了(PR #469) |
| 4 | identity(typed IDs) | 型✓ + lint✓ | `.semgrep/typed-id-rules` | 完了 |
| 5 | client/daemon 境界(読み取り) | semgrep ~60 ルール✓ | `make lint` + CI | 完了 |
| 6 | cancellation/stop 意味論(W7/W8, PR close terminal) | **disposition 決定済み**(run-state-machine.md §6): W7=ladder と統合、W8=v2 統合 | terminal guard + A1 凍結 | W7 統合済(D-B1); W8 は v2 |
| 7 | エラー伝播 × append(fail-fast) | **修正済み (A2, 2026-06-12)** | `no-ignored-status-append` ルール | 完了 |
| P-1 | 【未解決の構造問題】agent 状態の報告チャネルが存在せず、run status を画面・死活のプロキシ観測から推論している。§9 gate / §10 attestation / §11 pr-attach / §12 L-S3 は全て同一問題が生む補正条項 family(= 症状一覧) | 問題として記録(2026-07-13)。**解決方向は決定済み**(2026-07-13 所有者裁定): orch backend の doeff-sessionhost 化(agent 状態は daemon wire で第一級報告)。ただし**大型バージョンアップとして beta 後に延期 — beta 期間中は現行構造で運用** | 各条項の property tests(症状の再発防止のみ。問題自体は塞げていない) | beta 後のメジャー版(backend 差し替え)。前提工事 = ADR-DOE-AGENTS-006(doeff PR #517/#518/#519、merge 済み)。条項 family が成長を続ける事実をこの表で可視化する |
| P-2 | 【未解決の構造問題】session lifecycle に第一級の表現が無い。現状は agent_session artifact と session_reaped note の比較式(ADR-0005 L-S3 ラッチ)という影の状態機械 | 問題として記録(2026-07-13)。**解決方向は決定済み**(同上): doeff-sessionhost の conversation/incarnation 台帳(会話 = 耐久エンティティ、session 行 = incarnation、generation/lineage 第一級)を輸入する。**beta 後に延期** | L-S3 property tests(現行 2 イベント範囲のみ) | beta 後のメジャー版。beta 中に Stage 3(revive)が世代連鎖を本格化させるため、影の状態機械の成長はこの表で監視する |

進捗ログ:
- 2026-06-12 Phase A 完了(PR #463): A1 ルール+46 サイト凍結 / A2 fail-fast 化 / A3 AGENTS.md routing。
- 2026-06-12 Phase B 部分完了: B2 disposition 決定(run-state-machine.md §6)、B3 StopRun 動詞化(TUI のローカル mux kill + client append を撤去 — リモート run の stop が壊れていたバグも同時修正)。B3 ResolveRun は §6 の式で choice space を閉じ、verified issue 化。
- 2026-06-12 C1/C3 の選択空間を閉鎖(run-state-machine.md §7 D-C1/D-C3、L7/L3' 制定)。実装は C フェーズ。
- 2026-06-12 Phase C 実装完了(PR #467): C3 = L3' 対称化(I7 解消)、C1 = initialRunCore で fold 導出(I2/I5 解消)。C2/C4 は issue 委譲済み(inv-monitor-queued-orphans / inv-fire-status-change-all)。
- 2026-06-12 Phase D1 完了(PR #466): step_sim_test.go 観測スクリプト replay、5 シナリオで matrix と実装の一致を検証。D2(過去 issue の観測語彙カバレッジ)は未着手。
- 2026-06-12 Phase E1+E3 完了(PR #465 / #468): docs/design/worker-lease.md(LW1-LW5、LL1-LL5 draft)+ `.semgrep/worker-lease-mutation/` ルール(lease map 変異を worker_plane.go に限定、現状違反ゼロの純粋な柵)。残: E2(LL1-LL5 の property test 化)。
- 2026-06-12 Phase E2 完了(PR #469): LL 法テスト一式 + **LL3 実バグ修正**(acknowledgeWorkerLease が確定済み判定を無条件上書きしていた → first-verdict-wins の冪等 no-op に)。LL1/LL5 は既存テストでカバー済みと判定。
- 2026-06-12 Phase D2 完了(PR #470): docs/design/observation-coverage.md — 過去 21 修正の転記で完全表現 61.9% + 補語彙で 100%。判定: 語彙は健全、欠落はすべて infrastructure 面(意図的スコープ境界)。O11–O14 拡張バックログを記録。
- 2026-06-12 委譲 3 issue 完遂: inv-resolve-run-verb(PR #473)/ inv-monitor-queued-orphans(PR #471, I6 解消)/ inv-fire-status-change-all(PR #472, I8 解消 + L8 制定)。全 merge・issue resolved。
- 2026-06-13 **Phase B1 完了**: W2–W5/W7 の 37 サイトを O8 観測化(stepLaunchProgress + reportLaunchProgress)、`commitRunStatus` 抽出で status イベント構築点が正確に 1 箇所に。launch 失敗 verdict に `launch_<step>` reason(L9 制定)。whitelist 47 → 10(残 = W6/W8/ResolveRun/W9×4/W10 repair — 全て §6 disposition 済み)。run-state-machine.md §1/§2/§3/§5/§6/§7 D-B1 同梱。
- 残作業: v2(4 重 bootstrap 制御フローの統合、W6/W8 の step 統合)。whitelist のさらなる縮小はそこで。
- 2026-07-07 run-state-machine.md §9/§10 設計追加(issue beta-stalled-agent-detection): §9 = interactive gate 検出(O4e 派生読み取り + kind 付き streak、waiting + `gate_<kind>` reason、L10a–c、D-G1 (status,reason) no-op)、§10 = observer attestation(O3 精緻化 + ObserverID/GoneClass、L11a–d、L7' 強度単調性、I9)。2026-07-07 の 2 インシデント(codex login 2h 停滞 / TMUX 汚染 worker による誤 failed)の choice space を閉鎖。実装 issue は human review 後(両方コア面 = frontier + human)。
- 2026-07-13 **P-1/P-2 の処遇決定 + ADR-0005 進捗**: backend の doeff-sessionhost 化(P-1/P-2 根治)は方向合意済みだが beta 後の大型バージョンアップに延期(所有者裁定)— beta 中は現行 backend で ADR-0005 残 stage(2 reaper / 3 revive / 4 観測面)を完遂して配布する。ADR-0005 は Stage 0(PR #531)/ Stage 1(PR #532)/ T2c L-S3 吸収(PR #533)が merge 済み。
- 2026-07-11 **WL#2 静的ガード制定**(issue run-state-c2-derived-state-static-guard、侵食監査 2026-07-02 ギャップ #1): `.semgrep/derived-state-guard/` — ① `run-event-vocabulary-closed`(event 語彙 status/phase/artifact/test/note を閉集合化 — D-C1 却下案 (b) counter-as-event の新種別ルートを封鎖)、② `no-monitor-counter-in-event`(runCore フィールド値の event コンストラクタ/AppendEvent への構文的流入を禁止)、③ `run-core-hydration-surface`(runCore のフィールド代入・非ゼロリテラル構築を step.go に confine — 却下案 (c) snapshot 読み戻しの形を封鎖。凍結 nosemgrep 5 箇所: applyStep commit / PRRecorded revert / gitVerdict adapter / noteRunFeedback ×2、以後縮小のみ)。加えて golden 分類テスト(derived_state_guard_test.go): runCore/RunState の全フィールドを fold-derived / ephemeral / scheduling に強制分類 + struct tag 全面禁止(直列化準備のトリップワイヤ)。テスト専用だった `RunState.recordInputReading` は monitor_test.go へ移設(本番の変異面を縮小)。fixture 検証 + 実コード走査を `make lint` に配線。

## 推奨順序と箱

```
Day 0       : A1 + A2 + A3 + F1   (柵。これが終わるまでコア委譲は控える)
Day 1–3     : B1 → B3 → B2        (whitelist が空になるまで)
Day 4–6     : C1 → C3 → C2/C4/C5  (C2/C4 は codex 並行可)
Day 7–8     : D1 → D2
並行(任意) : E1 → E2 → E3        (次に lease を触る前に E1 だけでも)
```

codex に切り出せる verified issue 候補: A2 実装、B3 の機械的移植、
C2、C4、D2 転記。それ以外(A1, B1, B2 判断, C1, C3, D1 設計, E 全部)は
frontier + human。

## Watchlist 外 enforcement の相互参照

2026-07-02 の侵食監査で「宣言なき検査」と分類されたルール群は、上の
coupling-core watchlist とは別の spec/issue から導入された回帰防止策である。
いずれも `.semgrep/architecture.yaml` から `make lint` に配線されている。
由来と現行ルールの対応を次に固定する。

| 由来 spec/issue | 現行 enforcement | watchlist との関係 |
|-----------------|------------------|--------------------|
| SPEC-12 cluster identity/global-listing routing | `daemon-proto-no-legacy-store-resolution-call` から `cli-validate-issue-files-must-use-listing-api` までの 28 ルール (`.semgrep/architecture.yaml:633-1197`; `cluster-identity-routing` / `cluster-global-listing` / `cluster-global-read`) | client/daemon 境界(#5)の cluster routing 拡張。独立した coupling core の宣言ではない |
| `orch-447` socket churn | `no-raw-unix-socket-dial` (`.semgrep/architecture.yaml:1718`) / `no-oneshot-health-probe-socket` (`.semgrep/architecture.yaml:1744`) | persistent connection の回帰防止 |
| `orch-450` PR-cache rate limit | `daemon-no-uncached-pr-lookup` (`.semgrep/architecture.yaml:1774`) / `daemon-no-uncached-pr-lookup-by-url` (`.semgrep/architecture.yaml:1797`) | daemon 所有の cache/rate-limit 境界 |
| identity-collision (`orch-422`) | `daemon-no-repoid-basename-fallback` (`.semgrep/architecture.yaml:1580`) / `daemon-no-implicit-repoid-fallback` (`.semgrep/architecture.yaml:1599`) | typed identity(#4)を repo ID 導出へ補強 |
| transport field-overload | `daemon-proto-no-message-overload-model` / `daemon-proto-no-message-overload-tmux` / `daemon-socket-no-message-model-consumption` / `daemon-socket-no-message-tmux-consumption` (`.semgrep/architecture.yaml:1510,1526,1542,1558`) | protobuf field の単一意味を守る transport contract |
| remote-session-control (GitHub #439) | `cli-no-direct-ssh-session-control` (`.semgrep/architecture.yaml:1821`) / `cli-no-remote-control-ssh-helpers` (`.semgrep/architecture.yaml:1841`) | host routing を daemon/worker protocol に限定 |
| exec boundary / legacy tmux (`orch-443`) | `no-legacy-tmux-import` (`.semgrep/architecture.yaml:1665`) / `no-raw-exec-in-boundary-layers` (`.semgrep/architecture.yaml:1686`) | multiplexer/exec の境界回帰防止 |

監査時点の control-agent-split 検査は旧 Go monitor CLI 専用だったため、
Go monitor 撤去(2026-07-10)と同時にルールも削除済みであり、現行 enforcement
としては数えない。
