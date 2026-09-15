# M10 parity report

Date: 2026-09-14

## Decision

Do not cut over from TypeScript yet. Keep the Go Telegram and dashboard
pilots side-by-side as the reversible runtime. The Go path has passed live
provider, Telegram, dashboard, canonical-session, semantic-memory, episodic-
memory, restart, cancellation, FIFO, backup, and rollback checks. Full
TypeScript parity is not claimed because several surfaces are intentionally
deferred below.

## Evidence ledger

| Area | TypeScript authority | Go evidence | Status |
|---|---|---|---|
| Session identity | `packages/coding-agent/src/core/session-manager.ts:709-717`, `:1973-1993` | `ResolveShared`; 74 Go tests; live Telegram, CLI, and dashboard use one conversation ID | accepted product extension |
| Agent/tool loop | `packages/agent/src/agent-loop.ts:155-371`, `packages/coding-agent/src/core/agent-session.ts:2177-2308`, `packages/coding-agent/src/core/session-manager.ts:486-527` | event-order, settled-lifecycle, tool, error, abort, steering-priority, usage/provider/model persistence, tool-status callback, provider-backed compaction, opt-in pre-prompt threshold, bounded overflow retry, atomic failure, and live provider tests | partial; full event payload and compaction settings parity open |
| Provider | `packages/ai/src/api/openai-completions.ts:699-717`, `:830-930`, `packages/ai/src/utils/overflow.ts:39-171` | deterministic SSE, response-body overflow classification, transient exclusion, reasoning-effort request shape, and live `z-ai/glm-5.3-flash` OpenRouter reply using Yen-owned credentials | partial; provider matrix open |
| Telegram | `packages/telegram/src/index.ts:27-33`, `:90-120`, `:193-270`, `:290-316`, `:430-470`, `:560-700`, `packages/telegram/src/format.ts` | dedicated unit, bounded Telegram attachment download with Yen artifact storage/read-tool note, photo-to-OpenAI image content, persisted opt-in `/on tool call(s)` and `/off tool call(s)` detail toggle with bounded previews, tool-status message plus in-place final edit, concurrent poll-batch dispatch with FIFO runtime serialization, escaped classic HTML fallback for headings/lists/code/links/emphasis, text/caption fallback for messages and quoted replies, case-insensitive `stop`/`halt`/`/stop`/`/cancel` controls (plus Yen `/abort`), bounded `sendRichMessage` attempt with classic fallback, standalone-section splitting and threaded replies, TypeScript 4,000-character Unicode chunking, reply-context/target tests, cancellable typing-action loop, valid token, live reply, owner guard | partial; live image/document acceptance and rich/long-reply/reply acceptance deferred |
| Dashboard | `packages/dashboard/src/index.ts:320-400`, `:530-550`, `packages/dashboard/src/public/app.js:417-458` | Go auth unit tests plus VPS acceptance: health 200, unauthenticated API 401, login 200, cookie 200, Bearer 200; TypeScript-shaped session list/read-back for shared dashboard/Telegram links; SSE delta/tool_call/tool_result/usage/done/error protocol tests; live request routed to canonical conversation | partial; browser/UI rendering acceptance remains open |
| Semantic memory | `packages/coding-agent/src/core/tools/memory.ts:18-59`, `packages/coding-agent/src/core/memory-store.ts:9-19`, `packages/coding-agent/src/core/memory-consolidation.ts:28-70`, `:257-330`, `:401-406`, `:500-525` | conversation-scoped `save_note`/`remember`; live favorite-color and probe read-back; consolidation applies only the pinned closed edge vocabulary; opt-in trigger, 70-message ceiling, 100,000-character tail cap, separate state, failure cooldown, scoped provider retry, required episode timestamps, optional JSON-object request mode, and live trigger/checkpoint/episode read-back with the separate Yen provider key | partial; broader consolidation output parity remains open |
| Session read-back/migration | `packages/coding-agent/src/core/session-manager.ts:31-104`, `:284-355`, `:357-370`, `:397-430`, `:986-1022` | Go fixtures migrate v1/v2 JSONL to v3, skip malformed lines before/after the header, preserve v2 tree links and extension/message payloads, assign v1 IDs/parents, convert `hookMessage`, project the active parent-linked branch, and read flat v3 compaction entries; 120 Go tests | partial; remaining malformed/truncated-stream edge cases open |
| Episodic memory | `packages/coding-agent/src/core/episodic-store.ts:85-` | eight live records (`cli`, `telegram`, `dashboard`) in shared SQLite store; restart read-back | partial; historical episodic migration remains explicit-only |
| Operations | deployed TypeScript systemd units | Go systemd units, health, journald, verified backup, rollback/restore, and isolated installer acceptance with temporary root/fake systemctl | partial; real-host rollout remains separate |

## Accepted and deferred differences

- Full normalized live-trace equivalence is not claimed beyond the recorded
  golden/local traces and live acceptance results.
- Dashboard rendering, full branch navigation, historical episodic migration,
  the full provider matrix,
  and unported tools remain deferred by scope. Dashboard API authentication is
  implemented behind the Yen dashboard token, and authenticated live
  acceptance is now recorded. Live consolidation acceptance is now recorded; the
  full provider matrix and historical episodic migration remain deferred.
- A fresh-host installer remains deferred; the side-by-side systemd layout,
  backup, health, journald, and rollback procedure are verified on the pilot
  VPS.

This is a no-cutover parity decision, not a claim of total feature parity.
The TypeScript runtime remains operational, Go remains a reversible pilot, and
no decommission or irreversible cutover is authorized.
