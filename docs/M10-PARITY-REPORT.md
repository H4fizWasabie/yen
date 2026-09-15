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
| Agent/tool loop | `packages/agent/src/agent-loop.ts:155-371`, `packages/coding-agent/src/core/agent-session.ts:2177-2308`, `packages/coding-agent/src/core/session-manager.ts:486-527` | event-order, settled-lifecycle, tool, error, abort, steering-priority, usage/provider/model persistence, provider-backed compaction, opt-in pre-prompt threshold, bounded overflow retry, atomic failure, and live provider tests | partial; full event payload and compaction settings parity open |
| Provider | `packages/ai/src/api/openai-completions.ts:699-717`, `packages/ai/src/utils/overflow.ts:39-171` | deterministic SSE, response-body overflow classification, transient exclusion, and live `z-ai/glm-5.3-flash` OpenRouter reply | partial; provider matrix open |
| Telegram | `packages/telegram/src/index.ts:140-150`, `:183-285`, `:340-385`, `:630-700` | dedicated unit, Unicode chunking and reply-context/target tests, valid token, live reply, owner guard, `/stop` unit test | partial; rich rendering, images, and live long-reply/reply acceptance deferred |
| Dashboard | `packages/dashboard/src/index.ts:66-115`, `:530-550` | Go auth unit tests plus isolated local acceptance: health 200, unauthenticated API 401, login 200, cookie 200, Bearer 200; live request routed to canonical conversation | partial; VPS/UI acceptance open |
| Semantic memory | `packages/coding-agent/src/core/tools/memory.ts:18-59`, `packages/coding-agent/src/core/memory-store.ts:9-19`, `packages/coding-agent/src/core/memory-consolidation.ts:28-70` | conversation-scoped `save_note`/`remember`; live favorite-color and probe read-back; consolidation applies only the pinned closed edge vocabulary; opt-in trigger, 70-message ceiling, separate state, and failure cooldown are covered by Go tests | partial; full prompt/retry matrix and live consolidation acceptance open |
| Session read-back/migration | `packages/coding-agent/src/core/session-manager.ts:31-104`, `:284-355`, `:986-1022` | Go fixture migrates v1/v2 JSONL to v3, preserves extension/message payloads, assigns IDs/parent links, converts `hookMessage`, and reads flat v3 compaction entries; 93 Go tests | partial; full branch semantics and malformed-line recovery remain open |
| Episodic memory | `packages/coding-agent/src/core/episodic-store.ts:85-` | eight live records (`cli`, `telegram`, `dashboard`) in shared SQLite store; restart read-back | partial; historical episodic migration remains explicit-only |
| Operations | deployed TypeScript systemd units | Go systemd units, health, journald, verified backup, rollback/restore, and isolated installer acceptance with temporary root/fake systemctl | partial; real-host rollout remains separate |

## Accepted and deferred differences

- Full normalized live-trace equivalence is not claimed beyond the recorded
  golden/local traces and live acceptance results.
- Dashboard rendering, full branch navigation, historical episodic migration,
  the full provider matrix,
  and unported tools remain deferred by scope. Dashboard API authentication is
  implemented behind `THEOSES_DASHBOARD_TOKEN`, but authenticated live
  acceptance remains open.
- A fresh-host installer remains deferred; the side-by-side systemd layout,
  backup, health, journald, and rollback procedure are verified on the pilot
  VPS.

This is a no-cutover parity decision, not a claim of total feature parity.
The TypeScript runtime remains operational, Go remains a reversible pilot, and
no decommission or irreversible cutover is authorized.
