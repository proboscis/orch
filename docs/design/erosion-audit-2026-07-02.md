# 侵食監査 2026-07-02 — watchlist ⇔ 機械検査ギャップ(orch)

> 4リポジトリ横断監査の orch 分。宣言済み核・法則・不変量を全数列挙し、機械検査の存在と
> 「CI で実際に走るか」を突き合わせた(read-only、Fable 5 監査エージェント)。
> 横断まとめ: `~/repos/erosion-audit-2026-07-02-cross-repo.md`
> 対応 issue: `run-state-c2-derived-state-static-guard`(frontier)/ `coupling-core-watchlist-refresh`(codex)

**結論: ENFORCED 27 / PARTIAL 4 / EXISTS-NOT-WIRED 0 / DOC-ONLY 1(意図的)。4リポジトリ中最良。**
CI(quality.yaml: 毎 PR + main push で `make lint` + `go test ./...` + `-race`)、pre-commit、
semgrep fixture 検証(`semgrep test`)まで配線済み。roadmap 進捗ログの主張(PR #463–#473、
nosemgrep 47→10)はコードと突合して実在を確認 — 誇張なし。

---

## enforcement 基盤(確認済み)

- `.semgrep/architecture.yaml`(~90ルール)+ `run-status-write-surface` / `typed-id-rules` /
  `worker-lease-mutation`(各ルールに `ruleid:`/`ok:` 注釈つき fixture)。
- `make lint` = fixture 検証(`semgrep test`)→ 実コード走査(`semgrep --error --config .semgrep/ ./internal/`)。
- law テスト: `internal/daemon/step_test.go`(L1a–L9)、`worker_plane_law_test.go`(LL1–LL5)、
  `monitor_publish_test.go`(I8)、`step_sim_test.go`(過去インシデント5本の replay)。
- 自己防衛: SpecInventorySpec 相当は Go 側では `go test ./...` の全収集で担保。

## 判定サマリ

- **ENFORCED(27)**: C1 run-status 遷移核、C3 worker lease、C4 typed IDs、C5 client/daemon 境界、
  C7 error×append fail-fast、L1a–L9 全法則、I2/I3/I5/I6/I7/I8、LW1–5、LL1–5。
- **PARTIAL(4)**: C2、C6、I1、I4(下記)。
- **DOC-ONLY(1)**: O11–O14(明示的に deferred — ギャップではない)。

## 残ギャップ(優先順)

1. **C2 — store-of-record vs derived-state 核に静的ガードなし。** L7(step_test.go:458)は
   現行 fold 導出の restart 透過性という「振る舞い」を証明するが、delegate が永続カウンタ/
   スナップショットミラーを追加しても機械的に止まらない — 正しくミラーされていれば L7 を通過する。
   守りは AGENTS.md:81 の人間 tripwire のみ。watchlist 表の #2 行も「守り: なし」のまま古い。
   → 対処: runCore の永続フィールド追加を confine する semgrep(run-status-write-surface パターン踏襲)
   または「counter を event として書かない」fold-conformance テスト。核直上の law 制定なので
   **frontier + 人間 + watchlist 行改訂を1セット**。
2. **I4 — 重複イベント非蓄積の検査が6書き込み経路中 W1 のみ。** 凍結 legacy writer
   (W6/W8/W9×4/W10)は `nosemgrep` で write-surface ルールの盲点。Phase B v2(4つの
   命令型 bootstrap 制御フローの崩し込み)で解消予定 — roadmap は正直に未了と記録。
3. **C6 — stop(W8)/feedback(W6)が property-tested な step() 核の外。** 遷移方針は
   run-state-machine §6 で決定済み(v2 disposition)、統合が未了。畳めば I4 盲点も同時に縮む。
4. **文書内不整合(小)** — watchlist 表 #2/#3 行が「守り: なし(実装待ち)/law 候補ドラフト」の
   ままだが、直下の進捗ログ(PR #467 C1、PR #469 LL)とテスト実在が実装完了を示す。表の更新漏れ。

## 宣言なき検査(orphan、欠陥ではない)

architecture.yaml の約50ルールは watchlist 外の別 spec/issue 由来(すべて `make lint` で実行):
SPEC-12 cluster identity/global-listing routing(:729–1105、~25ルール)、orch-447 socket-churn(:1989)、
orch-450 PR-cache rate-limit(:2047)、identity-collision basename(:1803)、transport field-overload(:1733)、
control-agent-split(:2094)、remote-session-control(:2116)、legacy-tmux/raw-exec 境界(:1932)。
roadmap からの相互参照がない → watchlist 更新時にクロスリファレンス節を追加する。

## 他リポジトリへの示唆

orch の「fixture つき semgrep + law テスト + anti-drop 収集 + CI 配線」の構成は、
ACP(CI ゼロ)・doeff(cargo feature 未配線)・a sibling project(CI が消滅パスを参照)が
そのまま輸入すべき参照実装。特に SpecInventorySpec 型の anti-drop ratchet は
ACP の Hy runner `TEST-MODULES` 手動リスト問題への直接の解。
