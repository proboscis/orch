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
  (socket.go ×10, proto_handler.go ×4, internal/monitor/monitor.go ×2,
  cli/tick.go, cli/repair.go)。これを守る semgrep ルールが無い。
- proto_handler.go の 4 箇所が `_ = st.AppendEvent(...)` でエラー握り潰し。
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

### A2. AppendEvent エラー握り潰しの根絶【決定=frontier / 実装=codex】
- 対象: `proto_handler.go:1513,1564,1606,1657` の `_ = st.AppendEvent(...)`
- 決定事項(frontier): 失敗時の伝播先 — リクエスト失敗にするか、
  error artifact + ログにするか。fail-fast 原則に従い前者を既定とする。
- 成果物: 4 箇所の修正 + `.semgrep/` ルール `no-ignored-append-event`
  (`_ = $ST.AppendEvent(...)` パターン禁止)。
- exit: 握り潰しが lint で再発不能。

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
- `socket.go` / `proto_handler.go` の起動段階遷移
  (queued → booting → running/failed)を launch-progress 観測(O8)として
  step に流す。imperative bootstrap は効果(effect)として残し、
  遷移の決定だけを step に移す。
- 対象: socket.go:1896,2970,3459,3474,3487,3516,3564,3593,4321,4340 /
  proto_handler.go:1513,1564,1606,1657
- exit: whitelist から daemon 内の該当行が消える。

### B2. W6 feedback / W8 stop / W9 master projection / W10 external append【frontier 判断 → codex 実装可】
- 各サイトを「step の観測に統合する」か「明示隔離(理由を
  run-state-machine.md に追記)」のどちらかに決定する。
  どちらでもよいが、**未決定のまま残すことだけが禁止**。
- W10(external append、`socket.go handleAppendEvent`)は外界橋
  (law 境界)として隔離が妥当な候補 — `CanTransitionStatus` が唯一の守り
  であることを明記。

### B3. クライアント面からの status 書き込み撤去【API 設計=frontier / 移植=codex】
- 対象: `internal/monitor/monitor.go:594`(canceled), `:749`(done)、
  `cli/tick.go:163`、`cli/repair.go:207`
- クライアントが status イベントを組み立てるのをやめ、意味のある
  daemon API 動詞(CancelRun / ResolveRun / Repair)に置き換える。
  「状態計算は daemon」の境界の書き込み版。
- 成果物: API 動詞 + `.semgrep/` ルール `client-no-status-append`
  (internal/cli, internal/monitor での Type:"status" append 禁止)。
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
- 通知発火を step の効果実行器に移し、O4 経路だけでなく
  すべての遷移で listener が発火することを test 化。B 完了後は自然に
  落ちてくる作業。

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
| 1 | run status 遷移 | ADR✓ law✓(tested) runtime△ **static✓ (A1, 2026-06-12)** | `run-status-write-surface` ルール + 凍結 whitelist | B で whitelist→0 |
| 2 | RunState vs event log(導出状態) | 決定済み **D-C1**(run-state-machine.md §7): 導出可能フィールドは fold、収束カウンタは ephemeral-by-law (L7) | なし(実装待ち) | C1 実装 |
| 3 | worker lease 所有権/ライフサイクル | 調査完了(E1 doc)、law 候補 5 件ドラフト | なし | E2/E3 |
| 4 | identity(typed IDs) | 型✓ + lint✓ | `.semgrep/typed-id-rules` | 完了 |
| 5 | client/daemon 境界(読み取り) | semgrep ~60 ルール✓ | `make lint` + CI | 完了 |
| 6 | cancellation/stop 意味論(W7/W8, PR close terminal) | **disposition 決定済み**(run-state-machine.md §6): W7=ladder と統合、W8=v2 統合 | terminal guard + A1 凍結 | B1 後 |
| 7 | エラー伝播 × append(fail-fast) | **修正済み (A2, 2026-06-12)** | `no-ignored-status-append` ルール | 完了 |

進捗ログ:
- 2026-06-12 Phase A 完了(PR #463): A1 ルール+46 サイト凍結 / A2 fail-fast 化 / A3 AGENTS.md routing。
- 2026-06-12 Phase B 部分完了: B2 disposition 決定(run-state-machine.md §6)、B3 StopRun 動詞化(TUI のローカル mux kill + client append を撤去 — リモート run の stop が壊れていたバグも同時修正)。B3 ResolveRun は §6 の式で choice space を閉じ、verified issue 化。
- 2026-06-12 C1/C3 の選択空間を閉鎖(run-state-machine.md §7 D-C1/D-C3、L7/L3' 制定)。実装は C フェーズ。
- 2026-06-12 Phase C 実装完了(PR #467): C3 = L3' 対称化(I7 解消)、C1 = initialRunCore で fold 導出(I2/I5 解消)。C2/C4 は issue 委譲済み(inv-monitor-queued-orphans / inv-fire-status-change-all)。
- 2026-06-12 Phase D1 完了(PR #466): step_sim_test.go 観測スクリプト replay、5 シナリオで matrix と実装の一致を検証。D2(過去 issue の観測語彙カバレッジ)は未着手。
- 2026-06-12 Phase E1+E3 完了(PR #465 / #468): docs/design/worker-lease.md(LW1-LW5、LL1-LL5 draft)+ `.semgrep/worker-lease-mutation/` ルール(lease map 変異を worker_plane.go に限定、現状違反ゼロの純粋な柵)。残: E2(LL1-LL5 の property test 化)。
- 2026-06-12 Phase E2 完了(PR #469): LL 法テスト一式 + **LL3 実バグ修正**(acknowledgeWorkerLease が確定済み判定を無条件上書きしていた → first-verdict-wins の冪等 no-op に)。LL1/LL5 は既存テストでカバー済みと判定。
- 2026-06-12 Phase D2 完了(PR #470): docs/design/observation-coverage.md — 過去 21 修正の転記で完全表現 61.9% + 補語彙で 100%。判定: 語彙は健全、欠落はすべて infrastructure 面(意図的スコープ境界)。O11–O14 拡張バックログを記録。
- 残作業: **B1 のみ**(launch ladder W2–W5 統合 — whitelist 45→0。socket.go の 4 重 ladder 解体なのでフル文脈の新セッションで着手すること)+ issue 4 件の消化(inv-never-alive-local-unknown は PR #467 で解決済み、run 起動不要)。

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
