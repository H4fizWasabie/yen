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
| Session identity | `packages/coding-agent/src/core/session-manager.ts:709-717`, `:1973-1993` | `ResolveShared`; 73 Go tests; live Telegram, CLI, and dashboard use one conversation ID | accepted product extension |
| Agent/tool loop | `packages/agent/src/agent-loop.ts:155-371` | event-order, tool, error, abort, and queue tests; live provider smoke | partial |
| Provider | `packages/ai/src/api/openai-completions.ts:699-717` | deterministic SSE tests and live `z-ai/glm-5.3-flash` OpenRouter reply | partial; provider matrix open |
| Telegram | `packages/telegram/src/index.ts:340-385`, `:630-700` | dedicated unit, valid token, live reply, owner guard, `/stop` unit test | partial; rendering/image parity deferred |
| Dashboard | `packages/dashboard/src/index.ts:299-306` | dedicated localhost unit, `/healthz`, live request routed to canonical conversation | partial; UI/auth parity open |
| Semantic memory | `packages/coding-agent/src/core/tools/memory.ts:18-59` | conversation-scoped `save_note`/`remember`; live favorite-color and probe read-back | partial; consolidation matrix open |
| Episodic memory | `packages/coding-agent/src/core/episodic-store.ts:85-` | six live records in shared SQLite store; restart read-back | partial; historical migration open |
| Operations | deployed TypeScript systemd units | Go systemd units, health, journald, verified backup, rollback and restore | partial; fresh-host installer deferred |

## Accepted differences before any future cutover review

- Compare normalized live traces against the pinned TypeScript traces.
- Decide whether accepted differences around dashboard rendering, auth,
  migration, and unported tools are acceptable.

Until those gates are closed, the TypeScript runtime remains the operational
fallback and no decommission or irreversible cutover is authorized.
