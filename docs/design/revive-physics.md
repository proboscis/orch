# revive の実 CLI 物理(ADR-0005 R5 の校正記録)

sessionhost 化前の orch が依存する、実物 claude / codex CLI の resume 物理の
実測記録。`internal/agent/claude.go` / `codex.go` の resume argv と、revive の
identity 意味論(同一 id・世代のみ増加)の根拠をここに凍結する。

測定日 2026-07-13(claude はこの日の Claude Code、model haiku-4-5 で実測。
codex は同日の doeff ADR-DOE-AGENTS-006 Phase 0 プローブ、codex 0.144.1)。

## claude

- **`--resume <id>` は同一 transcript に追記する(identity 不変)**。実測:
  `--session-id <uuid1>` で鋳造したセッションに対し
  `claude --print --resume <uuid1> "..."` を実行しても、transcript ファイルは
  `<CLAUDE_CONFIG_DIR>/projects/<mangled cwd>/<uuid1>.jsonl` の 1 つのまま
  (新ファイルは現れない)。2 ターン目の応答本文が 1 ターン目の内容を明示的に
  参照しており、文脈継承もファイル内容(両ターンのメッセージが同一ファイルに
  連続)で証明される。
- **`--session-id` は `--resume` と併用できない**。実測エラー:
  `--session-id can only be used with --continue or --resume if --fork-session
  is also specified.`
  → `--fork-session` は「親の文脈を継いだ**新しい会話**」(doeff Phase 0 で
  実測: 親と別の新 UUID transcript が生まれる)であり、revive(同一会話の
  継続)の意味論ではない。**revive では使わない**。
- 含意: revive の claude argv は `claude ... --resume <AgentSessionID>`。
  identity は起動前から確定している stored fact であり、発見腕は不要。

## codex

- **`resume <id>` は既存 rollout を継続する**(doeff Phase 0 実測: 前セッション
  で覚えさせた語を resume 後に回答、既存 rollout ファイル継続)。usage は
  `codex [OPTIONS] <COMMAND> [ARGS]` — root options が subcommand に先行する
  ため、argv は `codex --yolo ... resume <id>` の形。
- revive 後の identity は同一 id の見込みだが、orch は T1b の発見腕
  (rollout 先頭行 `session_meta` の cwd-match)を boot 後に再実行して
  観測で裏取りした id を generation+1 の agent_session artifact に記録する
  (推測ではなく観測に基づく事実として)。

## 意味論への帰結(ADR-0005 R5)

- **conversation identity は世代を跨いで不変**: `agent_session` artifact の
  id は revive で変わらず、generation だけが incarnation の通し番号として
  +1 される。L-S3 ラッチは「reaped 世代 >= 現世代」で判定されるので、
  generation+1 の記録がラッチを解消する。
- doeff ADR-DOE-AGENTS-006 の存在論(会話 = 耐久エンティティ、session 行 =
  incarnation)と同型。backend を doeff-sessionhost に差し替える将来の
  メジャー版でも、この意味論はそのまま移行する。
