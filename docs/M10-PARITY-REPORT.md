# M10 parity report

Date: 2026-09-15

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
| Session identity | `packages/coding-agent/src/core/session-manager.ts:709-717`, `:1973-1993` | `ResolveShared`; dashboard lookup fallback through an existing Telegram link; 74 Go tests; live Telegram, CLI, and dashboard use one conversation ID | accepted product extension |
| Agent/tool loop | `packages/agent/src/agent-loop.ts:155-371`, `:399-426`, `:445-530`, `packages/agent/src/types.ts:17-28`, `packages/ai/src/types.ts:442-449`, `packages/coding-agent/src/core/usage-totals.ts:44`, `packages/coding-agent/src/core/agent-session.ts:2177-2308`, `packages/coding-agent/src/core/compaction/compaction.ts:163-167`, `:178-250`, `:234-335`, `:131-139`, `:267-270`, `packages/coding-agent/src/core/settings-manager.ts:854-876` | event-order, settled-lifecycle, tool, error, abort, steering-priority, parallel independent tool execution, usage/provider/model and response-metadata persistence, usage-backed context estimation with error/aborted exclusion, persisted thinking/message and assistant error-message context conversion, image-aware context estimation, automatic-compaction enabled toggle, opt-in max-history-turns compaction, agent/turn/message/tool lifecycle callbacks with message and turn payloads, provider stream events for text/thinking/tool-call start-delta-end with partial assistant messages, tool-status and tool-execution lifecycle callbacks, length-limited tool-call safety, recoverable length-stop compaction/retry, provider-backed compaction, token-budget compaction cut points, opt-in context-window plus reserve-token threshold (defaults 16,384 reserve and 20,000 recent tokens), bounded overflow retry, and live provider tests | partial; provider-specific stream metadata and remaining compaction settings parity open |
| Provider | `packages/ai/src/api/openai-completions.ts:699-717`, `:830-930`, `:362-465`, `:523-551`, `:632-647`, `:1260-1370`, `:1553-1575`, `packages/ai/src/utils/overflow.ts:39-171` | deterministic SSE including explicit-null finish reasons, response-body overflow classification, transient exclusion, reasoning-effort request shape, reasoning-field and ordered `reasoning_details` replay with session persistence, OpenAI and Anthropic response ID/model and raw finish-reason preservation, finish-reason error mapping, and live `z-ai/glm-5.3-flash` OpenRouter reply using Yen-owned credentials | partial; provider matrix open |
| Telegram | `packages/telegram/src/index.ts:27-33`, `:90-120`, `:193-270`, `:290-316`, `:430-470`, `:560-700`, `packages/telegram/src/format.ts` | dedicated unit, bounded Telegram attachment download with Yen artifact storage/read-tool note, photo-to-OpenAI image content, persisted opt-in `/on tool call(s)` and `/off tool call(s)` detail toggle with bounded previews, tool-status message plus in-place final edit, concurrent poll-batch dispatch with FIFO runtime serialization, escaped classic HTML fallback for headings/lists/code/links/emphasis, text/caption fallback for messages and quoted replies, case-insensitive `stop`/`halt`/`/stop`/`/cancel` controls (plus Yen `/abort`), bounded `sendRichMessage` attempt with classic fallback, standalone-section splitting and threaded replies, TypeScript 4,000-character Unicode chunking, reply-context/target tests, cancellable typing-action loop, valid token, live reply, owner guard | partial; live image/document acceptance and rich/long-reply/reply acceptance deferred |
| Dashboard | `packages/dashboard/src/index.ts:235-251`, `:289-292`, `:320-400`, `:530-550`, `packages/dashboard/src/public/app.js:1-220`, `:417-458` | Go auth unit tests plus VPS acceptance: health 200, unauthenticated API 401, login 200, cookie 200, Bearer 200; same-origin browser shell for login, recent-first session list/history with first-user-message titles, real message count and last-entry metadata, new session, send with bounded reply context, stop, and SSE delta/tool progress; dashboard can read/send a conversation with only a Telegram registry link; TypeScript-shaped session list/read-back for shared dashboard/Telegram links; SSE delta/tool_call/tool_result/usage/done/error protocol tests; live request routed to canonical conversation | partial; browser rendering has HTTP shell coverage but no live browser acceptance yet |
| Semantic memory | `packages/coding-agent/src/core/tools/memory.ts:18-59`, `packages/coding-agent/src/core/memory-store.ts:9-19`, `packages/coding-agent/src/core/memory-consolidation.ts:28-70`, `:257-330`, `:401-406`, `:443-464`, `:500-525`, `packages/coding-agent/src/core/compaction/compaction.ts:816-874` | conversation-scoped `save_note`/`remember`; live favorite-color and probe read-back; non-overlapping consolidation provider passes; active-branch timestamped, bounded tool-call/result (including failed-result status), bash-execution, and summary transcript entries; consolidation applies only the pinned closed edge vocabulary; opt-in trigger, 70-message ceiling, 100,000-character tail cap, separate state, failure cooldown, scoped provider retry, required episode timestamps, optional JSON-object request mode; tolerant per-member fact/edge/related-id filtering; string-aware trailing-comma, raw-control, and invalid-escape repair; distillation parser/provider seam with confidence filtering and array/object response support, integrated as non-blocking dropped-memory extraction during compaction; and live trigger/checkpoint/episode read-back with the separate Yen provider key | partial; broader structured-output and extraction parity remain open |
| Session read-back/migration | `packages/coding-agent/src/core/session-manager.ts:31-104`, `:284-355`, `:357-370`, `:397-430`, `:986-1022`, `:1431-1451`, `:1708-`, `packages/coding-agent/src/core/agent-session-runtime.ts:357-` | Go fixtures migrate v1/v2 JSONL to v3, skip malformed lines before/after the header, read bounded 4 MiB JSONL entries, preserve image metadata and v2 tree links/extension/message payloads, assign v1 IDs/parents, convert `hookMessage`, preserve session-info names, create durable fork copies, validate/copy imported sessions, switch the active RPC session with a path override, project the active parent-linked branch, and read flat v3 compaction entries; 135 Go tests | partial; non-RPC channel session switching and malformed/truncated-stream behavior beyond the explicit bound remain open |
| Episodic memory | `packages/coding-agent/src/core/episodic-store.ts:85-` | eight live records (`cli`, `telegram`, `dashboard`) in shared SQLite store; restart read-back | partial; historical episodic migration remains explicit-only |
| Operations | deployed TypeScript systemd units | Go systemd units, health, journald, verified backup, rollback/restore, isolated installer acceptance with temporary root/fake systemctl, and repeated real-host side-by-side rollout/read-back | accepted for the side-by-side pilot; unprivileged fresh-host production rollout remains untested |

## Accepted and deferred differences

## Checkpoint update: 2026-09-15

Since the original pilot report, these open surfaces now have working Go
implementations and committed tests:

| Surface | Evidence | Remaining boundary |
|---|---|---|
| Cross-channel identity | `8629abf`, default `yen-primary` plus environment override | final live cross-channel replay trace |
| Dashboard branches | `bb39d8b`, durable branch marker, tree API, branch UI, reopen test | browser-level acceptance and richer tree presentation |
| RPC | `12a739c`, `808ff64`, JSONL server/client, prompt/steer/follow-up/events/state/messages/abort, bash, new-session/clone/export, branch/artifact/session-name/fork/import/switch/fork-message/stats/set-model/cycle-model/model-catalog/thinking-level/retry controls, queue-mode state/behavior and persistence, built-in command discovery, Unix socket | complete command matrix and extension UI protocol |
| CLI | `packages/coding-agent/src/modes/interactive/interactive-mode.ts:2833-2898`, `:5889-5910`, `/name`, `/session`, `/compact`, `/stats` command behavior | `cmd/theoses/main.go`, shared `session.Stats`, command tests; full TUI rendering, selectors, and remaining slash commands remain open |
| External tools | `f238e3b`, `5a11297`, HTTP sidecar, MCP HTTP, untrusted-content boundary, deferred search/call, `6979a81` MCP stdio; current Tavily and Cloudflare integrations use Yen-prefixed credentials/endpoints | full resource lifecycle and subprocess shutdown hardening |
| Resources and trust | `3b6aa0e`, `d7bc219`, `ca967c4`, bounded context, lazy skills, settings, trust store; steering/follow-up queue modes and auto-compaction now load from settings at startup | extension hooks and full settings parity |
| Provider configuration | `49794a2`, Yen-owned provider/model/key resolution; `d6c176c` installer key isolation; current boundary reads only `YEN_*` provider, model, image, data, and compaction variables and excludes global/Theoses credential fallbacks; `197901d`, `ab7677e`, `521b164` Anthropic Messages streaming, stop mapping, and retry; runtime `set_model`, OpenAI-compatible `/models` discovery/cycling, seven-level thinking control, and retry control for supported protocols | full provider protocols, catalog breadth, OAuth, and auth UI |
| Episodic migration | `a513983` | enriched legacy SQLite rows preserve optional workspace, conversation, channel, and turn metadata; broader historical migration policy remains explicit-only |

The current verified code gate is 217 tests, race tests, vet, and diff checks.
The side-by-side VPS read-back places the tested release at
`/opt/yen/releases/13f8d08`; both Yen units and both Theoses2 units remain
active, and Yen `/healthz` returns `{"ok":true}`.

- Full normalized live-trace equivalence is not claimed beyond the recorded
  golden/local traces and live acceptance results.
- Historical episodic migration, the full provider matrix and OAuth, extension
  hooks/commands/renderers/provider interception, and the
  complete RPC command matrix remain deferred by scope. Dashboard API
  authentication and basic branch navigation are implemented behind the Yen
  dashboard token; authenticated live acceptance is recorded. Live
  consolidation acceptance is recorded; historical episodic migration remains
  explicit-only.
- An unprivileged fresh-host production rollout remains deferred; the
  installer itself is covered by isolated temporary-root acceptance, while
  the side-by-side systemd layout, backup, health, journald, and rollback
  procedure are verified on the pilot VPS.

This is a no-cutover parity decision, not a claim of total feature parity.
The TypeScript runtime remains operational, Go remains a reversible pilot, and
no decommission or irreversible cutover is authorized.
