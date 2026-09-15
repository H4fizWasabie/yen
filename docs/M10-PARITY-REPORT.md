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
| Provider | `packages/ai/src/api/openai-completions.ts:699-717`, `:830-930`, `:362-465`, `:523-551`, `:632-647`, `:1260-1370`, `:1553-1575`, `packages/ai/src/api/mistral-conversations.ts:287-372`, `packages/ai/src/api/google-generative-ai.ts:80-260`, `packages/ai/src/api/openai-responses-shared.ts:597-750`, `packages/ai/src/api/theoses-messages.ts:345-470`, `packages/ai/src/utils/overflow.ts:39-171` | deterministic SSE including explicit-null finish reasons, response-body overflow classification, transient exclusion, Mistral native `reasoning_effort`, Mistral thinking-array deltas, and nine-character tool-call ID normalization, reasoning-field and ordered `reasoning_details` replay with session persistence, OpenAI and Anthropic response ID/model and raw finish-reason preservation, native Gemini REST streaming for text/thinking/function calls/usage/images, native OpenAI Responses streaming for text/function-call arguments/terminal usage/status and image input, and a Radius/theoses-messages adapter with Yen-owned credentials, SSE text/thinking/tool-call events, usage/stop metadata, and gateway model catalog; live `z-ai/glm-5.3-flash` OpenRouter reply remains the only live provider result | partial; remaining provider protocols, native multimodal edge cases, and OAuth open |
| Telegram | `packages/telegram/src/index.ts:27-33`, `:90-120`, `:193-270`, `:290-316`, `:430-470`, `:560-700`, `packages/telegram/src/format.ts` | dedicated unit, bounded Telegram attachment download with Yen artifact storage/read-tool note, photo-to-OpenAI image content, persisted opt-in `/on tool call(s)` and `/off tool call(s)` detail toggle with bounded previews, tool-status message plus in-place final edit, concurrent poll-batch dispatch with FIFO runtime serialization, escaped classic HTML fallback for headings/lists/code/links/emphasis, text/caption fallback for messages and quoted replies, case-insensitive `stop`/`halt`/`/stop`/`/cancel` controls (plus Yen `/abort`), bounded `sendRichMessage` attempt with classic fallback, standalone-section splitting and threaded replies, TypeScript 4,000-character Unicode chunking, reply-context/target tests, cancellable typing-action loop, valid token, live reply, owner guard | partial; live image/document acceptance and rich/long-reply/reply acceptance deferred |
| Dashboard | `packages/dashboard/src/index.ts:235-251`, `:289-292`, `:320-400`, `:530-550`, `packages/dashboard/src/public/app.js:1-220`, `:417-458` | Go auth unit tests plus VPS acceptance: health 200, unauthenticated API 401, login 200, cookie 200, Bearer 200; same-origin browser shell for login, recent-first session list/history with first-user-message titles, real message count and last-entry metadata, new session, send with bounded reply context, stop, and SSE delta/tool progress; dashboard can read/send a conversation with only a Telegram registry link; TypeScript-shaped session list/read-back for shared dashboard/Telegram links; persisted assistant thinking and durable bash-execution segments survive API projection and embedded rendering; SSE delta/tool_call/tool_result/usage/done/error protocol tests; live request routed to canonical conversation | partial; browser rendering has HTTP shell coverage but no live browser acceptance yet |
| Semantic memory | `packages/coding-agent/src/core/tools/memory.ts:18-59`, `packages/coding-agent/src/core/memory-store.ts:9-19`, `packages/coding-agent/src/core/memory-consolidation.ts:28-70`, `:257-330`, `:401-406`, `:443-464`, `:500-525`, `packages/coding-agent/src/core/compaction/compaction.ts:816-874` | conversation-scoped `save_note`/`remember`; live favorite-color and probe read-back; non-overlapping consolidation provider passes; active-branch timestamped, bounded tool-call/result (including failed-result status), bash-execution, and summary transcript entries; consolidation applies only the pinned closed edge vocabulary; opt-in trigger, 70-message ceiling, 100,000-character tail cap, separate state, failure cooldown, scoped provider retry, required episode timestamps, optional JSON-object request mode; tolerant per-member fact/edge/related-id filtering; string-aware trailing-comma, raw-control, and invalid-escape repair; distillation parser/provider seam with confidence filtering and array/object response support, integrated as non-blocking dropped-memory extraction during compaction; and live trigger/checkpoint/episode read-back with the separate Yen provider key | partial; broader structured-output and extraction parity remain open |
| Session read-back/migration | `packages/coding-agent/src/core/session-manager.ts:31-104`, `:284-355`, `:357-370`, `:397-430`, `:986-1022`, `:1431-1451`, `:1708-`, `packages/coding-agent/src/core/agent-session-runtime.ts:357-` | Go fixtures migrate v1/v2 JSONL to v3, skip malformed lines before/after the header, read bounded 4 MiB JSONL entries, preserve image metadata and v2 tree links/extension/message payloads, assign v1 IDs/parents, convert `hookMessage`, preserve session-info names, create durable fork copies, validate/copy imported sessions, switch the active RPC session with a path override, project the active parent-linked branch, and read flat v3 compaction entries; 135 Go tests | partial; non-RPC channel session switching and malformed/truncated-stream behavior beyond the explicit bound remain open |
| Episodic memory | `packages/coding-agent/src/core/episodic-store.ts:85-` | eight live records (`cli`, `telegram`, `dashboard`) in shared SQLite store; restart read-back | partial; historical episodic migration remains explicit-only |
| Operations | deployed TypeScript systemd units | Go systemd units, health, journald, verified backup, rollback/restore, isolated installer acceptance with temporary root/fake systemctl, repeated real-host side-by-side rollout/read-back, and fresh 2026-09-15 backup `/var/backups/yen-20260915T093942Z.tgz` (mode 600) | accepted for the side-by-side pilot; unprivileged fresh-host production rollout remains untested |

## Accepted and deferred differences

## Checkpoint update: 2026-09-15

The settings boundary now deep-merges nested compaction and retry settings
from global and project files, migrates the oracle's legacy `queueMode`, and
applies the shared controls once to every CLI, Telegram, dashboard, and RPC
runner. TypeScript authority:
`packages/coding-agent/src/core/settings-manager.ts:85-175,420-423`.
Go evidence: `internal/settings/settings.go`,
`internal/runtime/runtime.go`, and startup wiring in `cmd/theoses*`; focused
settings/runtime tests pass. This closes the shared settings configuration
gap; interactive settings UI and the broader schema remain open.

Azure OpenAI Responses now has its own route, Azure resource/base-URL
resolution, and `api-key` authentication header instead of being treated as a
Bearer-authenticated OpenAI endpoint. TypeScript authority:
`packages/ai/src/providers/azure-openai-responses.ts` and
`packages/ai/src/api/azure-openai-responses.ts:196-250`.
Go evidence: `internal/provider/config.go`, `internal/provider/responses.go`,
and `TestAzureResponsesUsesAzureRouteAndAPIKeyHeader`; full AWS Bedrock
SigV4/credential-chain support remains open.

Resource context now also loads the configurable user agent directory and
`YEN.md` persona before repository context, matching Yen's global
resource pass. TypeScript authority:
`packages/coding-agent/src/core/resource-loader.ts:220-260` and
`packages/coding-agent/src/config.ts:517-523`. Go evidence:
`internal/codingagent/prompt.go` and
`TestContextMessageLoadsConfiguredAgentPersonaFirst`.

The built-in `read` tool now returns supported local images as rich data-image
content while preserving its existing bounded text path. TypeScript authority:
`packages/coding-agent/src/core/tools/read.ts`; Go evidence:
`internal/tools/read.go` and `TestReadToolReturnsImagesThroughRichResults`.

Settings-selected provider/model/thinking values now configure the live shared
runner when no explicit `YEN_*` override is present; explicit environment
configuration wins. This closes the prior read-but-not-applied settings bug.
Oracle authority: `packages/coding-agent/src/core/settings-manager.ts:85-175`
and `packages/coding-agent/src/core/agent-session-services.ts:141-183`.
Go evidence: `internal/runtime/runtime.go` and its provider precedence tests.

The side-by-side installer now forwards Azure resource/API-version settings and
`YEN_AGENT_DIR` through both service wrappers, preserving the provider and
global-resource boundaries in deployed runtimes. Evidence: `deploy/install-side-by-side.sh`
and `sh -n deploy/install-side-by-side.sh`.

Historical session-log backfill now reuses the bounded consolidation pipeline
in sequential chunks of at most 70 messages and leaves the live consolidation
checkpoint untouched, matching `packages/coding-agent/src/core/memory-consolidation.ts:585-650`.
Go evidence: `internal/memory/backfill.go` and
`TestBackfillFromSessionLogUsesBoundedPipelineWithoutLiveCheckpoint`.

The coding-agent bash boundary now records both the oracle's bounded automatic
working-note command log and a durable `bashExecution` session message with
the full command, output, exit code, cancellation, truncation, and
`excludeFromContext` fields. TypeScript authority:
`packages/coding-agent/src/core/agent-session.ts:3182-3253`; Go evidence:
`internal/tools/bash.go`, `internal/codingagent/bash.go`,
`internal/session/session.go`, and success/failure tests in
`internal/codingagent/working_note_test.go`. The full 261-test race/vet/diff
gate passes.

Bash capture now also enforces a bounded in-memory tail and spills oversized
output to a mode-600 temporary artifact, preserving `fullOutputPath` in the
session record. TypeScript authority:
`packages/coding-agent/src/core/bash-executor.ts:50-161` and
`packages/coding-agent/src/core/tools/truncate.ts:15-27,170-238`. Go evidence:
`internal/tools/bash.go` and
`internal/codingagent/working_note_test.go`; the full 264-test race/vet/diff
gate passes.

Provider configuration now recognizes the oracle's OpenCode Zen and OpenCode
Go provider IDs and their dedicated `OPENCODE_API_KEY` environment boundary,
using the pinned OpenAI-compatible endpoints. TypeScript authority:
`packages/ai/src/providers/opencode.ts`,
`packages/ai/src/providers/opencode-go.ts`, and their generated model catalogs.
Go evidence: `internal/provider/config.go` and
`internal/provider/config_test.go`; the full 263-test race/vet/diff gate
passes. Model-specific Anthropic/Google/Responses catalog metadata remains
open.

Anthropic streaming now preserves cache-read and cache-creation input-token
usage from `message_start`/`message_delta` events and computes total tokens.
TypeScript authority: `packages/ai/src/api/anthropic-messages.ts:600-610,740-760`.
Go evidence: `internal/provider/anthropic.go` and its streaming usage fixture in
`internal/provider/anthropic_test.go`; the full 264-test race/vet/diff gate
passes.

RPC `get_entries` now returns the active `leafId` and rejects an unknown
`since` entry instead of silently returning the full tree. TypeScript
authority: `packages/coding-agent/src/modes/rpc/rpc-mode.ts:632-650`; Go
evidence: `internal/rpc/server.go` and
`internal/rpc/server_test.go`; the full 265-test race/vet/diff gate passes.

RPC command discovery now includes trusted local skill resources as
`skill:<name>` commands with description and source-path metadata, matching
the oracle's skill portion of `get_commands`. TypeScript authority:
`packages/coding-agent/src/modes/rpc/rpc-mode.ts:650-690`; Go evidence:
`internal/codingagent/prompt.go`, `internal/rpc/server.go`, and
`internal/rpc/server_test.go`; the full 265-test race/vet/diff gate passes.

Prompt templates now load project `.theoses/prompts`, Yen config prompts, and
configured prompt directories; `/name args` expands `$1`, `$@`, and
`$ARGUMENTS` before the agent call, and RPC discovery advertises templates as
prompt commands. TypeScript authority:
`packages/coding-agent/src/core/prompt-templates.ts:136-266` and
`packages/coding-agent/src/core/agent-session.ts:1244-1249`. Go evidence:
`internal/codingagent/prompt_templates.go`, `internal/runtime/runtime.go`,
and focused template/RPC tests; the full 267-test race/vet/diff gate passes.

Skill commands now expand `/skill:name args` into the oracle-compatible XML
skill block with relative-reference guidance and trailing arguments before the
agent call. TypeScript authority:
`packages/coding-agent/src/core/agent-session.ts:1395-1419`; Go evidence:
`internal/codingagent/prompt_templates.go`, `internal/runtime/runtime.go`, and
focused expansion tests; the full 268-test race/vet/diff gate passes.

Prompt template expansion now matches the oracle's quoted argument parsing,
multi-digit positional arguments, defaults, and argument slices. TypeScript
authority: `packages/coding-agent/src/core/prompt-templates.ts:16-95`; Go
evidence: `internal/codingagent/prompt_templates.go` and focused substitution
tests; the full 269-test race/vet/diff gate passes.

Skill resource loading now falls back to the containing directory name when
`SKILL.md` omits frontmatter `name`, matching the oracle's declared-skill
loader. TypeScript authority:
`packages/coding-agent/src/core/skills.ts:307-323`; Go evidence:
`internal/codingagent/prompt.go`, `internal/codingagent/prompt_templates.go`,
and focused resource tests; the full 270-test race/vet/diff gate passes.

OpenAI-compatible Qwen, DeepSeek, and Together providers now use their native
reasoning request fields instead of the generic shape: `enable_thinking`,
DeepSeek `thinking`, and Together `reasoning.enabled` plus its effort field.
TypeScript authority:
`packages/ai/src/api/openai-completions.ts:857-900,917-925,1640-1650` and
the provider model compatibility data under
`packages/ai/src/providers/data/{qwen-token-plan,deepseek}.json`. Go evidence:
`internal/provider/openai.go` and
`TestOpenAICompatibleProvidersUseNativeThinkingFields`; the full gate passes
311 tests, race, vet, and diff checks. Per-model thinking maps and remaining
chat-template formats remain open.

Selected Baseten model families now send the oracle's
`chat_template_args.enable_thinking` field, while other Baseten models retain
the OpenAI-compatible reasoning path. TypeScript authority:
`packages/ai/src/api/openai-completions.ts:875-890` and
`packages/ai/src/providers/data/baseten.json`. Go evidence:
`internal/provider/openai.go` and
`TestBasetenChatTemplateModelsUseEnableThinkingArgument`; the full gate passes
312 tests, race, vet, and diff checks.

The agent loop now exposes opt-in `before` and `after` tool interception hooks:
the former can block a validated call with a policy reason, and the latter can
replace the executed text/images or error status before tool-result events and
persistence. TypeScript authority:
`packages/agent/src/types.ts:56-125,277-292` and
`packages/agent/src/agent-loop.ts:638-760`. Go evidence:
`internal/agent/loop.go` and `TestRunToolHooksCanBlockAndRewriteResults`; the
full 303-test race/vet/diff gate passes. Extension loading, command/rendering
hooks, and provider interception remain open.

The built-in `find` tool now supports recursive `**` glob patterns across
nested directories, matching the pinned file-search contract. TypeScript
authority: `packages/coding-agent/src/core/tools/find.ts`; Go evidence:
`internal/tools/fs_tools.go` and `TestFindToolSupportsRecursiveGlobstar`.

Since the original pilot report, these open surfaces now have working Go
implementations and committed tests:

| Surface | Evidence | Remaining boundary |
|---|---|---|
| Cross-channel identity | `8629abf`, default `yen-primary` plus environment override | live VPS read-back on 2026-09-15: `conversations.jsonl` maps Telegram, CLI, and dashboard adapter records to the same conversation ID; authenticated dashboard `/api/sessions` deduplicates the shared record; dashboard SSE prompt returned `CROSS_CHANNEL_OK` and a subsequent session GET read it back from that same ID | accepted side-by-side pilot; final Telegram-incoming replay remains open |
| Dashboard branches | `bb39d8b`, durable branch marker, tree API, branch UI, reopen test | browser-level acceptance and richer tree presentation |
| RPC | `12a739c`, `808ff64`, JSONL server/client, prompt/steer/follow-up/events/state/messages/abort with image payloads, bash, durable `bashExecution` records plus working-note logging, new-session/clone/export, branch/artifact/session-name/fork/import/switch/fork-message/stats/set-model/cycle-model/model-catalog/thinking-level/retry controls, queue-mode state/behavior and persistence, built-in command discovery, Unix socket, and `0451341` state fields for session file, compaction, and auto-compaction | complete command matrix and extension UI protocol |
| CLI | `packages/coding-agent/src/modes/interactive/interactive-mode.ts:2796-2800`, `:2899-2903`, `:2860-2870`, `:2889-2894`, `:5778-5865`, `:6206-6295`, `/settings`, `/reload`, `/name`, `/session`, `/compact`, `/stats`, `/model`, `/thinking`, `/retry`, `/trust`, `/tree`, `/artifacts`, `/export`, `/import`, `/clone`, `/new`, `/fork`, `/resume`, and `!`/`!!` bash behavior | `cmd/theoses/main.go`, shared `session.Stats`, provider control helpers, trust store, session tree/artifact read-back, direct bash output, durable `bashExecution` records, excluded-from-context flag, validated JSONL export/import with active reload, durable clone/fresh-session switching, argument-based `/fork <entry-id> [path]` and `/resume <path>` switching, settings read-back, session reload, and command tests; full TUI rendering, selectors, and remaining slash commands remain open |
| External tools | `f238e3b`, `5a11297`, HTTP sidecar, MCP HTTP, untrusted-content boundary, deferred search/call, `6979a81` MCP stdio; current Tavily and Cloudflare integrations use Yen-prefixed credentials/endpoints; runtime now closes optional external-tool resources after each turn and failed stdio initialization closes its child | full resource lifecycle, cross-turn resource reuse, and provider-specific extension interception |
| Resources and trust | `3b6aa0e`, `d7bc219`, `ca967c4`, bounded context, lazy skills, direct markdown skill discovery, disabled skills remain explicit-only, settings, trust store; steering/follow-up queue modes and auto-compaction now load from settings at startup | extension hooks and full settings parity |
| Provider configuration | `49794a2`, Yen-owned provider/model/key resolution; `d6c176c` installer key isolation; current boundary reads only `YEN_*` provider, model, image, data, and compaction variables and excludes global/Theoses credential fallbacks; `197901d`, `ab7677e`, `521b164` Anthropic Messages streaming, stop mapping, and retry; runtime `set_model`, OpenAI-compatible `/models` discovery/cycling, seven-level thinking control, and retry control for supported protocols; current registry covers the pinned OpenAI-compatible aliases including Ant Ling, Baseten, Cerebras, Fireworks, Hugging Face, Kimi, Moonshot CN, NVIDIA, Qwen token plans, Together, Xiaomi token plans, and Z.AI CN; `5c6d153` adds the OpenAI Codex Responses route, ChatGPT account-claim extraction, and experimental/account headers; `d4ce91b` adds GitHub Copilot’s OpenAI-compatible route, Yen-owned token, and dynamic initiator/intent/vision headers; `YEN_GOOGLE_API_KEY` selects the native Gemini adapter with `generateContent` model discovery; `9e0ba7b` adds the Google Vertex API-key path with Yen-only project/location/base URL configuration; MiniMax, MiniMax CN, and Vercel AI Gateway now resolve to native Anthropic Messages endpoints with Yen-owned keys; `89f1339` adds Cloudflare Workers AI and AI Gateway URL placeholder expansion and gateway authorization; Yen-owned atomic credential storage supports API-key/OAuth token records through explicit `YEN_AUTH_FILE` and now locks complete read–modify–write transactions across processes; native OpenAI and xAI selections now route through the Responses protocol, matching `packages/ai/src/providers/openai.ts` and `xai.ts` | full provider protocols, catalog breadth, provider-specific OAuth flows, and auth UI |
| Built-in document conversion | `packages/coding-agent/src/core/tools/convert-doc.ts:18-35`, `packages/coding-agent/test/convert-doc.test.ts` | `internal/codingagent/convert_doc.go` resolves the workspace path, invokes `markitdown`, trims output, preserves the empty-document response, and enforces the oracle's 2,000,000-byte stdout ceiling with `internal/codingagent/convert_doc_test.go` | accepted for the documented convert-doc contract; broader markitdown stderr/error text remains open |
| Built-in web search | `packages/coding-agent/src/core/tools/web-search.ts:17-71` | `internal/codingagent/web_search.go` posts Tavily's `{query,max_results:5}` request, tries filtered keys in order, formats answer/results, and snapshots the Yen key list at construction; `web_search_test.go` covers fallback and post-construction environment changes | accepted for the current Tavily contract; richer provider/resource interception remains open |
| Context resource discovery | `packages/coding-agent/src/core/resource-loader.ts:89-96` | `internal/codingagent/prompt.go` discovers `AGENTS.override.md`, `AGENTS.md`, `AGENTS.MD`, `CLAUDE.md`, and `CONTEXT.md` through the workspace ancestor chain; `prompt_test.go` covers the added names | partial; full resource-loader precedence, diagnostics, and extension discovery remain open |
| Dashboard authentication safety | `packages/dashboard/src/index.ts:82-90` | `internal/adapters/dashboard_http.go:89-103` now returns 503 when no access token is configured and 401 for invalid credentials; `dashboard_http_test.go` covers the unconfigured-token boundary | accepted for token-gated API access |
| Explorer turn budget | `packages/coding-agent/src/core/explorer.ts:42-49`, `:270-286` | `internal/codingagent/explore.go` enforces 8 quick-scan or 15 deep-map provider calls, returns an explicit `INCOMPLETE` answer at the ceiling, and preserves the read-only tool set; `explore_test.go` covers the hard stop | partial; pinned explorer model/catalog resolution, footer accounting, and provider interception remain open |
| Installer provider environment | `packages/ai/src/utils/provider-env.ts`, provider registrations under `packages/ai/src/providers/` | `deploy/install-side-by-side.sh` now forwards the complete Yen provider-key registry and native provider/auth settings through both `env -i` wrappers; `sh -n` and the full Go gate pass | accepted for environment forwarding; provider protocol/catalog parity remains partial |
| GitHub verification gate | repository `package.json` scripts and CI expectations | `.github/workflows/go.yml` runs `go test ./...`, race, vet, and PR/push diff checks; local equivalents pass | accepted for automated Go verification |
| Native-provider reasoning configuration | `packages/ai/src/api/google-generative-ai.ts`, `packages/ai/src/api/anthropic-messages.ts`, and provider option construction | `internal/provider/config.go` now carries `YEN_REASONING_EFFORT` into Google, Anthropic, MiniMax, and Vercel clients created by both configuration paths; `config_test.go` covers native providers | accepted for environment-driven reasoning configuration |
| Cloudflare provider routing | `packages/ai/src/providers/cloudflare-workers-ai.ts`, `cloudflare-ai-gateway.ts`, `cloudflare-auth.ts`, `packages/ai/src/api/cloudflare.ts` | `89f1339` adds Yen-owned Cloudflare credentials, account/gateway placeholder expansion, OpenAI-compatible routing, and `cf-aig-authorization`; `internal/provider/config_test.go` exercises the gateway SSE path; installer forwarding is covered by `deploy/install-side-by-side.sh` syntax validation | accepted for the implemented OpenAI-compatible path; full Cloudflare model catalog and non-compatible image/API paths remain open |
| Google Vertex credentials and routing | `packages/ai/src/providers/google-vertex.ts`, `packages/ai/src/api/google-vertex.ts` | `9e0ba7b` adds Yen-only Vertex project/location/base URL configuration and native Gemini-compatible streaming; `5bb3475` adds a Yen-owned bearer-token path without an API-key query; `internal/provider/config_test.go` verifies both endpoint forms; installer forwarding is covered by shell syntax validation | partial; full ADC/service-account credential discovery and Vertex catalog remain open |
| Amazon Bedrock Converse streaming | `packages/ai/src/providers/amazon-bedrock.ts`, `packages/ai/src/api/bedrock-converse-stream.ts`, `packages/ai/src/model-resolver.ts:15`, `packages/ai/src/env-api-keys.ts:167-182` | `internal/provider/bedrock.go` uses the pinned AWS SDK credential chain, profile/region/base-endpoint settings, bearer-token environment support from the SDK, ConverseStream text/tool/reasoning/usage events, replayable tool calls/results, PNG/JPEG data-image blocks, Claude reasoning signatures, opaque redacted-reasoning replay, and the AWS `ListFoundationModels` catalog; `bedrock_test.go` covers construction, replay, images, catalog mapping, and provider selection | partial; non-data image sources, static catalog metadata/auth UI, and live AWS acceptance remain open |
| GitHub Copilot headers | `packages/ai/src/providers/github-copilot.ts`, `packages/ai/src/api/github-copilot-headers.ts`, `packages/ai/src/api/openai-completions.ts:730-740` | `d4ce91b` adds Yen-owned `YEN_COPILOT_GITHUB_TOKEN`, OpenAI-compatible endpoint configuration, `X-Initiator`, `Openai-Intent`, and image-aware `Copilot-Vision-Request`; `internal/provider/config_test.go` verifies the streamed request and headers | partial; Copilot OAuth, catalog filtering, and Responses/Anthropic protocol variants remain open |
| OpenAI Codex Responses routing | `packages/ai/src/providers/openai-codex.ts`, `packages/ai/src/api/openai-codex-responses.ts:220-240,1541-1595` | `5c6d153` adds Yen-owned Codex token/base URL configuration, JWT account-claim extraction, `/codex/responses` routing, and account/experimental headers; `internal/provider/config_test.go` verifies the streamed request | partial; Codex OAuth login, WebSocket transport, compression, and full model catalog remain open |
| Episodic migration | `a513983` | enriched legacy SQLite rows preserve optional workspace, conversation, channel, and turn metadata; broader historical migration policy remains explicit-only |

The current verified code gate is 303 tests, race tests, vet, and diff checks.
The side-by-side VPS read-back places the tested release at
`/opt/yen/releases/0f97e38`; both Yen units and both Theoses2 units remain
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
