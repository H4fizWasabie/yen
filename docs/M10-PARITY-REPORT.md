# M10 parity report

Date: 2026-09-16

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
| Agent/tool loop | `packages/agent/src/agent-loop.ts:155-371`, `:399-426`, `:445-530`, `packages/agent/src/types.ts:17-28`, `packages/ai/src/types.ts:442-449`, `packages/coding-agent/src/core/usage-totals.ts:44`, `packages/coding-agent/src/core/agent-session.ts:2177-2308`, `packages/coding-agent/src/core/compaction/compaction.ts:163-167`, `:178-250`, `:234-335`, `:131-139`, `:267-270`, `packages/coding-agent/src/core/settings-manager.ts:854-876` | event-order, settled-lifecycle, tool, error, abort, steering-priority, parallel independent tool execution, usage/provider/model/thinking-signature and response-metadata persistence, usage-backed context estimation with error/aborted exclusion, persisted thinking/message and assistant error-message context conversion, image-aware context estimation, automatic-compaction enabled toggle, opt-in max-history-turns compaction, agent/turn/message/tool lifecycle callbacks, provider stream events for text/thinking/tool-call start-delta-end with partial assistant messages, tool-status and tool-execution lifecycle callbacks, length-limited tool-call safety, recoverable length-stop compaction/retry, provider-backed compaction, token-budget compaction cut points, opt-in context-window plus reserve-token threshold (defaults 16,384 reserve and 20,000 recent tokens), bounded overflow retry, and live provider tests | partial; provider-specific stream metadata and remaining compaction settings parity open |
| Provider | `packages/ai/src/api/openai-completions.ts:699-717`, `:830-930`, `:362-465`, `:523-551`, `:632-647`, `:1260-1370`, `:1553-1575`, `packages/ai/src/api/mistral-conversations.ts:287-372`, `packages/ai/src/api/google-generative-ai.ts:80-260`, `packages/ai/src/api/openai-responses-shared.ts:597-750`, `packages/ai/src/api/theoses-messages.ts:345-470`, `packages/ai/src/utils/overflow.ts:39-171` | deterministic SSE including explicit-null finish reasons, response-body overflow classification, transient exclusion, Mistral native `reasoning_effort`, Mistral thinking-array deltas, and nine-character tool-call ID normalization, reasoning-field and ordered `reasoning_details` replay with session persistence, OpenAI and Anthropic response ID/model and raw finish-reason preservation, native Gemini REST streaming for text/thinking/function calls/usage/images, native OpenAI Responses streaming for text/function-call arguments/terminal usage/status, image input, and reasoning-item summary/signature replay, and a Radius/theoses-messages adapter with Yen-owned credentials, SSE text/thinking/tool-call events, usage/stop metadata, and gateway model catalog; live `z-ai/glm-5.3-flash` OpenRouter reply remains the only live provider result | partial; remaining provider protocols, native multimodal edge cases, and OAuth open |
| Telegram | `packages/telegram/src/index.ts:27-33`, `:90-120`, `:193-270`, `:290-316`, `:430-470`, `:560-700`, `packages/telegram/src/format.ts` | dedicated unit, bounded Telegram attachment download with Yen artifact storage/read-tool note, photo-to-OpenAI image content, persisted opt-in `/on tool call(s)` and `/off tool call(s)` detail toggle with bounded previews, tool-status message plus in-place final edit, concurrent poll-batch dispatch with FIFO runtime serialization, escaped classic HTML fallback for headings/lists/code/links/emphasis, text/caption fallback for messages and quoted replies, case-insensitive `stop`/`halt`/`/stop`/`/cancel` controls (plus Yen `/abort`), bounded `sendRichMessage` attempt with classic fallback, standalone-section splitting and threaded replies, TypeScript 4,000-character Unicode chunking, reply-context/target tests, cancellable typing-action loop, valid token, live reply, owner guard | partial; live image/document acceptance and rich/long-reply/reply acceptance deferred |
| Dashboard | `packages/dashboard/src/index.ts:235-251`, `:289-292`, `:320-400`, `:530-550`, `packages/dashboard/src/public/app.js:1-220`, `:417-458` | Go auth unit tests plus VPS acceptance: health 200, unauthenticated API 401, login 200, cookie 200, Bearer 200; same-origin browser shell for login, recent-first session list/history with first-user-message titles, real message count and last-entry metadata, new session, send with bounded reply context, stop, and SSE delta/tool progress; dashboard can read/send a conversation with only a Telegram registry link; TypeScript-shaped session list/read-back for shared dashboard/Telegram links; persisted assistant thinking and durable bash-execution segments survive API projection and embedded rendering; SSE delta/tool_call/tool_result/usage/done/error protocol tests; live request routed to canonical conversation | partial; browser rendering has HTTP shell coverage but no live browser acceptance yet |
| Semantic memory | `packages/coding-agent/src/core/tools/memory.ts:18-59`, `packages/coding-agent/src/core/memory-store.ts:9-19`, `packages/coding-agent/src/core/memory-consolidation.ts:28-70`, `:257-330`, `:401-406`, `:443-464`, `:500-525`, `packages/coding-agent/src/core/compaction/compaction.ts:816-874` | conversation-scoped `save_note`/`remember`; live favorite-color and probe read-back; non-overlapping consolidation provider passes; active-branch timestamped, bounded tool-call/result (including failed-result status), bash-execution, and summary transcript entries; consolidation applies only the pinned closed edge vocabulary; opt-in trigger, 70-message ceiling, 100,000-character tail cap, separate state, failure cooldown, scoped provider retry, episode-object requirement with runtime timestamp defaults, optional JSON-object request mode; tolerant per-member fact/edge/related-id filtering; string-aware trailing-comma, raw-control, and invalid-escape repair; distillation parser/provider seam with confidence filtering and array/object response support, integrated as non-blocking dropped-memory extraction during compaction; and live trigger/checkpoint/episode read-back with the separate Yen provider key | partial; broader structured-output and extraction parity remain open |
| Session read-back/migration | `packages/coding-agent/src/core/session-manager.ts:31-104`, `:284-355`, `:357-370`, `:397-430`, `:986-1022`, `:1431-1451`, `:1708-`, `packages/coding-agent/src/core/agent-session-runtime.ts:357-` | Go fixtures migrate v1/v2 JSONL to v3, skip malformed lines before/after the header, read bounded 4 MiB JSONL entries, preserve image metadata and v2 tree links/extension/message payloads, assign v1 IDs/parents, convert `hookMessage`, preserve session-info names, create durable fork copies, validate/copy imported sessions, switch the active RPC session with a path override, project the active parent-linked branch, and read flat v3 compaction entries; 135 Go tests | partial; non-RPC channel session switching and malformed/truncated-stream behavior beyond the explicit bound remain open |
| Episodic memory | `packages/coding-agent/src/core/episodic-store.ts:85-` | eight live records (`cli`, `telegram`, `dashboard`) in shared SQLite store; restart read-back | partial; historical episodic migration remains explicit-only |
| Operations | deployed TypeScript systemd units | Go systemd units, health, journald, verified backup, rollback/restore, isolated installer acceptance with temporary root/fake systemctl, repeated real-host side-by-side rollout/read-back, and fresh 2026-09-15 backup `/var/backups/yen-20260915T093942Z.tgz` (mode 600) | accepted for the side-by-side pilot; unprivileged fresh-host production rollout remains untested |

## Accepted and deferred differences

Artifact catalog projection now follows the oracle's live-branch rules: stale
paths are omitted, repeated paths keep the newest entry, blank labels become
`document`, and lines use the `label (size bytes): path` shape. TypeScript
authority:
`packages/coding-agent/src/core/session-manager.ts:1237-1260`; Go evidence:
`internal/session/session.go` and
`TestArtifactCatalogUsesLiveNewestUniquePaths`. The focused test and full
369-test race/vet/diff gates pass.

The Go `convert_doc` tool now preserves non-empty `markitdown` stderr when the
conversion command fails, matching the oracle's `execFile` error propagation.
TypeScript authority: `packages/coding-agent/src/core/tools/convert-doc.ts:18-35`;
Go evidence: `internal/codingagent/convert_doc.go` and
`TestConvertDocIncludesMarkitdownStderrOnFailure`. The focused test and full
363-test race/vet/diff gates pass.

Explorer runs now enforce the oracle's input-token ceilings in addition to
turn ceilings: 200,000 tokens for `quick-scan` and 400,000 for `deep-map`.
The loop stops at the completed assistant/tool turn that consumes the budget,
matching `packages/coding-agent/src/core/explorer.ts:37-38,280-296` and its
`shouldStopAfterTurn` boundary. Go evidence is
`internal/codingagent/explore.go`, `internal/agent/loop.go`, and
`TestExploreStopsAfterBudgetedToolTurn`; 368 tests, race, vet, and diff gates
pass.

## Checkpoint update: 2026-09-16

Anthropic Messages streaming now preserves initial thinking signatures,
incremental `signature_delta` values, and redacted-thinking signatures for
multi-turn replay. TypeScript authority:
`packages/ai/src/api/anthropic-messages.ts:620-637,691-697`; Go evidence:
`internal/provider/anthropic.go` and
`TestAnthropicMessagesPreservesThinkingSignatureDeltas`.

The Radius/Theoses-messages adapter now preserves `thinking_end` content
signatures for multi-turn replay. TypeScript authority:
`packages/ai/src/api/theoses-messages.ts:57-64,228-233`; Go evidence:
`internal/provider/theoses_messages.go` and
`TestTheosesMessagesPreservesThinkingSignature`.

Radius terminal error events now preserve their reason, response ID, and usage
metadata, matching the TypeScript error-event converter. TypeScript
authority: `packages/ai/src/api/theoses-messages.ts:68-77,197-205`; Go
evidence: `internal/provider/theoses_messages.go` and
`TestTheosesMessagesPreservesErrorMetadata`.

Radius text block signatures now survive stream parsing, agent-loop response
propagation, session persistence, and replay. TypeScript authority:
`packages/ai/src/api/theoses-messages.ts:56,216-220`; Go evidence:
`internal/provider/theoses_messages.go`, `internal/runtime/runtime.go`, and
`TestTheosesMessagesPreservesTextSignature`.

Radius responses now preserve the served model metadata on the normalized
response, matching the adapter's assistant model field. TypeScript authority:
`packages/ai/src/api/theoses-messages.ts:134-144`; Go evidence:
`internal/provider/theoses_messages.go` and
`TestTheosesMessagesStreamsTextToolCallAndUsage`.

Bedrock Converse usage now preserves cache-read and cache-write token
breakdowns and falls back to the component sum when the service omits total
tokens. TypeScript authority:
`packages/ai/src/api/bedrock-converse-stream.ts:685-700`; Go evidence:
`internal/provider/bedrock.go` and
`TestBedrockUsagePreservesCacheTokenBreakdown`.

The agent loop now carries provider thinking signatures into persisted assistant
messages, preserving reasoning metadata for subsequent provider replay. Go
evidence: `internal/agent/loop.go` and
`TestRunCarriesProviderThinkingSignatureOnAssistantMessage`. The focused agent
race gate passes; the full race run remains blocked by the existing Telegram
poll-batch test timing out independently of this change.

Native OpenAI Responses reasoning output items now preserve their summary text
and serialized reasoning-item signature for stateless multi-turn replay, while
failed responses surface the provider's error. Go evidence:
`internal/provider/responses.go`,
`TestOpenAIResponsesPersistsReasoningItemSignature`, and
`TestOpenAIResponsesBackfillsReasoningSignatureFromTerminalOutput`,
`TestOpenAIResponsesReturnsResponseFailure`.
Captionless Telegram document guidance now names `convert_doc`, matching the
oracle's `noteFor` prompt and the tool intended for stored document artifacts.
TypeScript authority: `packages/telegram/src/index.ts:536-542`; Go evidence:
`internal/adapters/telegram_bot.go` and
`TestTelegramBotAttachmentNoteIncludesDocumentMetadata`. The focused test and
full 362-test race/vet/diff gates pass.

Telegram non-image documents and media now enter the canonical session artifact
log before the agent turn, so the same artifact catalog is visible to Telegram,
dashboard, and CLI consumers. This matches the pinned Telegram source at
`packages/telegram/src/index.ts:550-560,575-585`, where
`session.sessionManager.storeArtifact("telegram document", ...)` is used for
documents and other media. Go evidence is `internal/adapters/telegram_bot.go`
and `TestTelegramBotRecordsDocumentInSharedSessionArtifacts`; the focused test
and full 363-test race/vet/diff gates pass. Live incoming-file acceptance
remains open.

## Checkpoint update: 2026-09-15

Dropped-memory distillation now retries transient provider failures with the
same bounded exponential policy as the oracle's compaction path, while leaving
non-transient failures best-effort and non-blocking. TypeScript authority:
`packages/coding-agent/src/core/compaction/compaction.ts:816-879`. Go evidence:
`internal/memory/distillation.go` and
`TestDistillMemoryRetriesTransientProviderFailure`; channel retry UI remains
open.

Assistant calls now retry bounded transient provider failures independently of
provider HTTP retries, using the oracle's default three attempts and
exponential two-second base delay. Context-overflow, quota, and billing errors
remain on their dedicated paths. TypeScript authority:
`packages/ai/src/utils/retry.ts:146-250` and
`packages/coding-agent/src/core/agent-session.ts:3054-3135`. Go evidence:
`internal/provider/retry.go`, `internal/runtime/runtime.go`, and
`TestRunnerRetriesTransientAssistantFailure`; retry settings now include
`baseDelayMs`. The remaining retry gap is UI-specific cancellation/control
surfaces on channels that do not expose the generic event callback.

External HTTP/MCP tools are now initialized once by each Yen runtime and kept
open across turns, while turn-scoped built-ins are still closed after each
operation; process shutdown closes the persistent resources. This matches the
Theoses2 SDK's session-level external-tool sources in
`packages/coding-agent/src/core/sdk.ts:245-251,406-481`. Go evidence:
`internal/runtime/runtime.go`, `internal/codingagent/tools.go`, and
`TestRunnerKeepsPersistentToolsOpenAcrossTurns`.

Bedrock image conversion now accepts the oracle-supported GIF and WEBP data
formats in addition to PNG and JPEG/JPG, using the AWS SDK's native enum
values. TypeScript authority:
`packages/ai/src/api/bedrock-converse-stream.ts:1270-1285`; Go evidence:
`TestBedrockInputAcceptsGIFAndWEBPImages`.

Bedrock image conversion now maps the oracle-supported `image/jpg` MIME alias
to the AWS JPEG format, matching
`packages/ai/src/api/bedrock-converse-stream.ts:1270-1278`. Go evidence:
`TestBedrockInputAcceptsJPGImageAlias`.

Overflow recovery now also recognizes successful responses whose input plus
cache usage exceeds the configured context window, and Xiaomi-style `length`
stops with zero output when the window is at least 99% full. This matches
`packages/ai/src/utils/overflow.ts:144-165`; Go evidence is
`internal/provider/overflow.go`, `TestIsContextOverflowResponseDetectsSilentProviderOverflow`,
and `TestRunnerCompactsAfterSilentContextOverflow`.

Bedrock image conversion now fails fast for malformed data URLs, invalid
base64, and unsupported MIME types instead of silently dropping the image.
This matches `packages/ai/src/api/bedrock-converse-stream.ts:1268-1288`, where
unknown image types throw during request construction. Go evidence:
`internal/provider/bedrock.go` and
`TestBedrockInputRejectsUnsupportedOrInvalidImages`; normal and tool-result
image paths now share the same error boundary.

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
`YEN.md` persona before repository context, matching Yen's global resource
pass. TypeScript authority: `packages/coding-agent/src/core/resource-loader.ts`
and `packages/coding-agent/src/config.ts`. Linked worktrees now suppress the
main worktree's context file when the linked worktree has its own copy,
matching Theoses2's `findShadowedContextFile` path. Go evidence:
`internal/codingagent/prompt.go`, `TestContextMessageLoadsConfiguredAgentPersonaFirst`,
and `TestContextMessageSkipsMainWorktreeContextFromLinkedWorktree`.

The built-in `read` tool now returns supported local images as rich data-image
content while preserving its existing bounded text path. TypeScript authority:
`packages/coding-agent/src/core/tools/read.ts`; Go evidence:
`internal/tools/read.go` and `TestReadToolReturnsImagesThroughRichResults`.

Interactive CLI `!`/`!!` bash execution now persists the command outcome
metadata alongside its output and context-exclusion flag. TypeScript authority:
`packages/coding-agent/src/core/agent-session.ts:3182-3253`; Go evidence:
`cmd/theoses/main.go` and `TestInteractiveBashCommandPersistsExitMetadata`.

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
316 tests, race, vet, and diff checks. Per-model thinking maps and remaining
chat-template formats remain open.

Selected Baseten model families now send the oracle's
`chat_template_args.enable_thinking` field, while other Baseten models retain
the OpenAI-compatible reasoning path. TypeScript authority:
`packages/ai/src/api/openai-completions.ts:875-890` and
`packages/ai/src/providers/data/baseten.json`. Go evidence:
`internal/provider/openai.go` and
`TestBasetenChatTemplateModelsUseEnableThinkingArgument`; the full gate passes
316 tests, race, vet, and diff checks.

OpenAI-compatible streaming now falls back to usage embedded in a choice when
the provider omits the top-level usage chunk, preserving input, output,
reasoning, cache, and total-token accounting. TypeScript authority:
`packages/ai/src/api/openai-completions.ts:1260-1305`. Go evidence:
`internal/provider/openai.go` and
`TestOpenAICompletionsUsesChoiceUsageWhenTopLevelUsageIsAbsent`; the full gate
passes 316 tests, race, vet, and diff checks.

Overflow classification now includes the oracle's provider-specific error
signatures for Bedrock, Gemini, xAI, Groq, Copilot, llama.cpp, LM Studio,
MiniMax, Kimi, Mistral, DS4, z.ai, DashScope/Qwen, and generic context-length
failures, while retaining rate-limit exclusions. TypeScript authority:
`packages/ai/src/utils/overflow.ts:39-171`. Go evidence:
`internal/provider/overflow.go` and
`TestIsContextOverflowErrorMatchesProviderPatterns`; the full gate passes 312
tests, race, vet, and diff checks.

Anthropic message conversion now matches the oracle's content shape: text-only
messages use a string, image messages use ordered text/image blocks, and
image-only messages receive the `(see attached image)` placeholder. TypeScript
authority: `packages/ai/src/api/anthropic-messages.ts:116-160`. Go evidence:
`internal/provider/anthropic.go` and
`TestAnthropicMessagesUsesOracleContentShapeForImages`; the full gate passes
317 tests, race, vet, and diff checks.

The agent loop now exposes opt-in `before` and `after` tool interception hooks:
the former can block a validated call with a policy reason, and the latter can
replace the executed text/images or error status before tool-result events and
persistence. TypeScript authority:
`packages/agent/src/types.ts:56-125,277-292` and
`packages/agent/src/agent-loop.ts:638-760`. Go evidence:
`internal/agent/loop.go` and `TestRunToolHooksCanBlockAndRewriteResults`; the
full 316-test race/vet/diff gate passes. Extension loading, command/rendering
hooks, and provider interception remain open.

The built-in `find` tool now supports recursive `**` glob patterns across
nested directories, and `grep` applies the same path-aware glob filtering,
truncates long match lines, and matches the pinned file-search contract.
TypeScript authority:
`packages/coding-agent/src/core/tools/find.ts` and `grep.ts`; Go evidence:
`internal/tools/fs_tools.go`, `TestFindToolSupportsRecursiveGlobstar`, and
`TestGrepToolMatchesRecursiveGlobstarPaths`, plus
`TestGrepToolTruncatesLongMatchingLines`.

The built-in `ls` and `find` tools now apply the oracle's 6 KiB output ceiling
without cutting through a filename line. TypeScript authority:
`packages/coding-agent/src/core/tools/ls.ts`, `find.ts`, and `truncate.ts`;
Go evidence: `internal/tools/fs_tools.go` and
`TestFileSearchToolsBoundOutput`.

File-search result-count limits now emit the oracle's actionable notice for
`ls` and `find`, including the requested larger limit. Go evidence:
`internal/tools/fs_tools.go` and `TestFileSearchToolsReportResultLimit`.

The `ls` limit now applies after the oracle's case-insensitive sort, so limiting
a directory does not change which first entry is returned. Go evidence:
`internal/tools/fs_tools.go` and `TestListToolSortsBeforeApplyingLimit`.

The `grep` match limit now emits the oracle's refinement hint when it stops
collecting matches. TypeScript authority:
`packages/coding-agent/src/core/tools/grep.ts:348-375`; Go evidence:
`internal/tools/fs_tools.go` and `TestGrepToolReportsMatchLimit`.

Grep match lines now use the oracle's `path:line: text` spacing. TypeScript
authority: `packages/coding-agent/src/core/tools/grep.ts:327-334`; Go evidence:
`internal/tools/fs_tools.go` and `TestGrepToolUsesOracleMatchSpacing`.

Grep context lines now use the oracle's `path-line- text` shape while matched
lines retain `path:line: text`. Go evidence: `internal/tools/fs_tools.go` and
`TestGrepToolFormatsContextLinesLikeOracle`.

Grep match limits now count matching lines rather than emitted context lines,
so the complete context block is retained for the final allowed match. Go
evidence: `internal/tools/fs_tools.go` and
`TestGrepToolLimitCountsMatchesNotContextLines`.

The `find` and `grep` tools now ask Git to exclude paths ignored by repository
`.gitignore` rules, matching the oracle's default file-search behavior while
retaining searches outside Git repositories; `grep` also skips binary files as
the oracle's ripgrep JSON path does and removes standalone carriage returns
from displayed lines. Go evidence: `internal/tools/fs_tools.go`,
`TestFindToolRespectsGitignore`, `TestGrepToolSkipsBinaryFiles`, and
`TestGrepToolRemovesCarriageReturns`.

The `ls` tool now follows entry targets while listing and skips entries that
cannot be statted, matching the oracle's broken-symlink behavior. TypeScript
authority: `packages/coding-agent/src/core/tools/ls.ts:143-160`; Go evidence:
`internal/tools/fs_tools.go` and `TestListToolSkipsBrokenSymlinks`.

OpenAI-compatible Z.AI providers now use the oracle's native reasoning wire
fields: enabled `thinking` with `clear_thinking: false` and `tool_stream: true`,
instead of the generic `reasoning.effort` field. TypeScript authority:
`packages/ai/src/api/openai-completions.ts:844-855,1640-1650` and Z.AI model
compatibility data under `packages/ai/src/providers/data/zai.json`. Go evidence:
`internal/provider/openai.go` and `TestZAIUsesThinkingAndToolStreamFields`;
the full gate now passes 307 tests, race, vet, and diff checks. Other
model-specific OpenAI-compatible thinking formats remain open.

Context, skill, and prompt-template loaders now strip a UTF-8 BOM before
parsing or displaying resource content, matching the oracle's `stripBom`
boundary. Go evidence: `internal/codingagent/prompt.go`,
`internal/codingagent/prompt_templates.go`, and the BOM regression tests.

Explicit skill directories from Yen settings now work for both skill discovery
and `/skill:name` expansion, keeping advertisement and execution on the same
resource set. Go evidence: `TestSkillPromptExpansionUsesConfiguredSkillDirs`.

Context discovery now stops at the first existing candidate in each directory,
even when that higher-priority file is empty, matching the oracle's precedence
rule. Go evidence: `TestContextMessageEmptyHigherPriorityFileShadowsLowerPriority`.

Settings-driven bash execution now applies the oracle's `shellPath` and
`shellCommandPrefix` to agent tools and CLI `!` commands. TypeScript authority:
`packages/coding-agent/src/core/settings-manager.ts:968-1005` and
`packages/coding-agent/src/core/agent-session.ts:3195-3204`. Go evidence:
`TestBashToolAppliesConfiguredShellPathAndPrefix` and
`TestSessionToolsLoadShellSettings`.

Since the original pilot report, these open surfaces now have working Go
implementations and committed tests:

| Surface | Evidence | Remaining boundary |
|---|---|---|
| Cross-channel identity | `8629abf`, default `yen-primary` plus environment override | live VPS read-back on 2026-09-15: `conversations.jsonl` maps Telegram, CLI, and dashboard adapter records to the same conversation ID; authenticated dashboard `/api/sessions` deduplicates the shared record; dashboard SSE prompt returned `CROSS_CHANNEL_OK` and a subsequent session GET read it back from that same ID | accepted side-by-side pilot; final Telegram-incoming replay remains open |
| Dashboard branches | `bb39d8b`, durable branch marker, tree API, branch UI, reopen test | browser-level acceptance and richer tree presentation |
| RPC | `12a739c`, `808ff64`, JSONL server/client, prompt/steer/follow-up/events/state/messages/abort with image payloads, bash, durable `bashExecution` records plus working-note logging, new-session/clone/export, branch/artifact/session-name/fork/import/switch/fork-message/stats/set-model/cycle-model/model-catalog/thinking-level/retry controls, queue-mode state/behavior and persistence, built-in command discovery, Unix socket, and `0451341` state fields for session file, compaction, and auto-compaction | complete command matrix and extension UI protocol |
| CLI | `packages/coding-agent/src/modes/interactive/interactive-mode.ts:2796-2800`, `:2828-2831`, `:2843-2851`, `:2873-2887`, `:2899-2903`, `:2860-2870`, `:2889-2894`, `:5778-5865`, `:6206-6295`, `/settings`, `/reload`, `/name`, `/session`, `/working-note`, `/compact`, `/stats`, `/model`, `/scoped-models`, `/thinking`, `/retry`, `/trust`, `/copy`, `/logout`, `/tree`, `/artifacts`, `/export`, `/import`, `/clone`, `/new`, `/fork`, `/resume`, and `!`/`!!` bash behavior | `cmd/theoses/main.go`, shared `session.Stats`, provider control helpers, trust store, session tree/artifact read-back, direct bash output, durable `bashExecution` records, excluded-from-context flag, provider catalog JSON read-back, clipboard copy, Yen auth credential listing/deletion, `/login openai-codex` and `/login github-copilot [enterprise-domain]` device login, validated JSONL export/import with active reload, durable clone/fresh-session switching, argument-based `/fork <entry-id> [path]` and `/resume <path>` switching, settings read-back, session reload, and command tests; full TUI rendering, selectors, and remaining slash commands remain open |
| External tools | `f238e3b`, `5a11297`, HTTP sidecar, MCP HTTP, untrusted-content boundary, deferred search/call, `6979a81` MCP stdio; current Tavily and Cloudflare integrations use Yen-prefixed credentials/endpoints; runtime now closes optional external-tool resources after each turn and failed stdio initialization closes its child | full resource lifecycle, cross-turn resource reuse, and provider-specific extension interception |
| Resources and trust | `3b6aa0e`, `d7bc219`, `ca967c4`, bounded context, lazy skills, direct markdown skill discovery, disabled skills remain explicit-only, settings, trust store; steering/follow-up queue modes and auto-compaction now load from settings at startup | extension hooks and full settings parity |
| Provider configuration | `49794a2`, Yen-owned provider/model/key resolution; `d6c176c` installer key isolation; current boundary reads only `YEN_*` provider, model, image, data, and compaction variables and excludes global/Theoses credential fallbacks; `197901d`, `ab7677e`, `521b164` Anthropic Messages streaming, stop mapping, and retry; runtime `set_model`, OpenAI-compatible `/models` discovery/cycling, seven-level thinking control, and retry control for supported protocols; current registry covers the pinned OpenAI-compatible aliases including Ant Ling, Baseten, Cerebras, Fireworks, Hugging Face, Kimi, Moonshot CN, NVIDIA, Qwen token plans, Together, Xiaomi token plans, and Z.AI CN; `5c6d153` adds the OpenAI Codex Responses route, ChatGPT account-claim extraction, and experimental/account headers; `d4ce91b` adds GitHub Copilot’s OpenAI-compatible route, Yen-owned token, and dynamic initiator/intent/vision headers; `YEN_GOOGLE_API_KEY` selects the native Gemini adapter with `generateContent` model discovery; `9e0ba7b` adds the Google Vertex API-key path with Yen-only project/location/base URL configuration; MiniMax, MiniMax CN, and Vercel AI Gateway now resolve to native Anthropic Messages endpoints with Yen-owned keys; `89f1339` adds Cloudflare Workers AI and AI Gateway URL placeholder expansion and gateway authorization; Yen-owned atomic credential storage supports API-key/OAuth token records through explicit `YEN_AUTH_FILE` and now locks complete read–modify–write transactions across processes; native OpenAI and xAI selections now route through the Responses protocol, matching `packages/ai/src/providers/openai.ts` and `xai.ts` | full provider protocols, catalog breadth, provider-specific OAuth flows, and auth UI |
| Built-in document conversion | `packages/coding-agent/src/core/tools/convert-doc.ts:18-35`, `packages/coding-agent/test/convert-doc.test.ts` | `internal/codingagent/convert_doc.go` resolves the workspace path, invokes `markitdown`, trims output, preserves the empty-document response, and enforces the oracle's 2,000,000-byte stdout ceiling with `internal/codingagent/convert_doc_test.go` | accepted for the documented convert-doc contract; broader markitdown stderr/error text remains open |
| Built-in web search | `packages/coding-agent/src/core/tools/web-search.ts:17-71` | `internal/codingagent/web_search.go` posts Tavily's `{query,max_results:5}` request, tries filtered keys in order, formats answer/results, and snapshots the Yen key list at construction; `web_search_test.go` covers fallback and post-construction environment changes | accepted for the current Tavily contract; richer provider/resource interception remains open |
| Context resource discovery | `packages/coding-agent/src/core/resource-loader.ts:89-96` | `internal/codingagent/prompt.go` discovers `AGENTS.override.md`, `AGENTS.md`, `AGENTS.MD`, `CLAUDE.md`, and `CONTEXT.md` through the workspace ancestor chain; `prompt_test.go` covers the added names | partial; full resource-loader precedence, diagnostics, and extension discovery remain open |
| Dashboard authentication safety | `packages/dashboard/src/index.ts:82-90` | `internal/adapters/dashboard_http.go:89-103` now returns 503 when no access token is configured and 401 for invalid credentials; `dashboard_http_test.go` covers the unconfigured-token boundary | accepted for token-gated API access |
| Explorer turn budget | `packages/coding-agent/src/core/explorer.ts:37-38`, `:270-296` | `internal/codingagent/explore.go` enforces 8 quick-scan or 15 deep-map provider calls plus 200K/400K input-token ceilings, returns an explicit `INCOMPLETE` answer at the ceiling, and preserves the read-only tool set; `internal/agent/loop.go` stops after the completed budgeted tool turn; `explore_test.go` covers both hard stops | partial; pinned explorer catalog hydration, footer accounting, and provider interception remain open |
| Installer provider environment | `packages/ai/src/utils/provider-env.ts`, provider registrations under `packages/ai/src/providers/` | `deploy/install-side-by-side.sh` now forwards the complete Yen provider-key registry and native provider/auth settings through both `env -i` wrappers; `sh -n` and the full Go gate pass | accepted for environment forwarding; provider protocol/catalog parity remains partial |
| GitHub verification gate | repository `package.json` scripts and CI expectations | `.github/workflows/go.yml` runs `go test ./...`, race, vet, and PR/push diff checks; local equivalents pass | accepted for automated Go verification |
| Native-provider reasoning configuration | `packages/ai/src/api/google-generative-ai.ts`, `packages/ai/src/api/anthropic-messages.ts`, and provider option construction | `internal/provider/config.go` now carries `YEN_REASONING_EFFORT` into Google, Anthropic, MiniMax, and Vercel clients created by both configuration paths; `config_test.go` covers native providers | accepted for environment-driven reasoning configuration |
| Cloudflare provider routing | `packages/ai/src/providers/cloudflare-workers-ai.ts`, `cloudflare-ai-gateway.ts`, `cloudflare-auth.ts`, `packages/ai/src/api/cloudflare.ts` | `89f1339` adds Yen-owned Cloudflare credentials, account/gateway placeholder expansion, OpenAI-compatible routing, and `cf-aig-authorization`; `internal/provider/config_test.go` exercises the gateway SSE path; installer forwarding is covered by `deploy/install-side-by-side.sh` syntax validation | accepted for the implemented OpenAI-compatible path; full Cloudflare model catalog and non-compatible image/API paths remain open |
| Google Vertex credentials and routing | `packages/ai/src/providers/google-vertex.ts`, `packages/ai/src/api/google-vertex.ts` | `9e0ba7b` adds Yen-only Vertex project/location/base URL configuration and native Gemini-compatible streaming; `5bb3475` adds a Yen-owned bearer-token path without an API-key query; stored credential environment, gcloud project fallback, ADC/service-account bearer exchange, and paginated `generateContent` catalog discovery are covered by `internal/provider/config_test.go` and `google_test.go`; installer forwarding is covered by shell syntax validation | partial; auth UI, static catalog metadata, and live Vertex acceptance remain open |
| Amazon Bedrock Converse streaming | `packages/ai/src/providers/amazon-bedrock.ts`, `packages/ai/src/api/bedrock-converse-stream.ts`, `packages/ai/src/model-resolver.ts:15`, `packages/ai/src/env-api-keys.ts:167-182` | `internal/provider/bedrock.go` uses the pinned AWS SDK credential chain, profile/region/base-endpoint settings, bearer-token environment support from the SDK, ConverseStream text/tool/reasoning/usage events, cache read/write token accounting, replayable tool calls/results, PNG/JPEG data-image blocks, Claude reasoning signatures, opaque redacted-reasoning replay, and the AWS `ListFoundationModels` catalog; `bedrock_test.go` covers construction, replay, images, usage, catalog mapping, and provider selection | partial; non-data image sources, static catalog metadata/auth UI, and live AWS acceptance remain open |
| GitHub Copilot headers | `packages/ai/src/providers/github-copilot.ts`, `packages/ai/src/api/github-copilot-headers.ts`, `packages/ai/src/api/openai-completions.ts:730-740`, `packages/ai/src/auth/oauth/github-copilot.ts:22-30,208-325,342-359,430-460` | `d4ce91b` adds Yen-owned `YEN_COPILOT_GITHUB_TOKEN`, OpenAI-compatible endpoint configuration, `X-Initiator`, `Openai-Intent`, and image-aware `Copilot-Vision-Request`; merged `2ef4b82` adds default and validated enterprise-domain device OAuth, `/login github-copilot`, token refresh, persisted enterprise/model metadata, and catalog filtering; `internal/auth/copilot_test.go` and provider configuration tests cover the flow and headers | partial; model enablement and Responses/Anthropic protocol variants remain open |
| OpenAI Codex Responses routing | `packages/ai/src/providers/openai-codex.ts`, `packages/ai/src/api/openai-codex-responses.ts:56-59,220-240,1417-1502,1575-1610`, `packages/ai/src/auth/oauth/openai-codex.ts:26-35,175-239,511-539` | `5c6d153` adds Yen-owned Codex token/base URL configuration, JWT account-claim extraction, `/codex/responses` routing, and account/experimental headers; merged `7e8b00c` adds headless device-code login and `/login openai-codex`; PR #126 adds expired-token refresh and persistence coverage; the Codex client now sends zstd-compressed SSE requests, decodes zstd responses, uses the WebSocket `response.create` transport, and falls back to SSE after a failed handshake; `internal/provider/responses_test.go` covers the wire contracts | partial; continuation caching, full model catalog, browser OAuth callback, and live acceptance remain open |
| Episodic migration | `a513983` | enriched legacy SQLite rows preserve optional workspace, conversation, channel, and turn metadata; broader historical migration policy remains explicit-only |

## Checkpoint update: 2026-09-16

GitHub Copilot device OAuth now accepts github.com and validated enterprise
domains, exchanges the GitHub token for a Copilot token, refreshes expired
stored credentials, and persists account model availability. Go evidence is
`internal/auth/copilot.go`, `internal/auth/copilot_test.go`, and the provider
configuration tests. Model enablement and alternate Copilot protocols remain
open.

Codex Responses now has the pinned oracle's compressed SSE request/response
path and WebSocket `response.create` path, with a failed-handshake fallback to
SSE. Go evidence is `internal/provider/responses.go` and
`TestOpenAICodexSSECompressesRequestAndDecodesResponse`,
`TestOpenAICodexWebSocketStreamsResponseCreate`, and
`TestOpenAICodexFallsBackToSSEWhenWebSocketHandshakeFails`. Continuation
caching, static catalog metadata, browser OAuth callback, and live Codex
acceptance remain open.

Vertex configuration now reads stored credential environment values, accepts
the gcloud project fallback, and discovers all paginated models that advertise
`generateContent`. Go evidence is `internal/provider/config.go`,
`internal/provider/google.go`, `TestGoogleVertexUsesStoredCredentialEnvironment`,
and `TestGoogleGenerativeAIListsAllCatalogPages`. Auth UI, static metadata, and
live Vertex acceptance remain open.

The current verified code gate is 427 tests, race tests, vet, and diff checks.
The 2026-09-16 side-by-side VPS read-back places the active pilot release at
`/opt/yen/releases/1ba4476`; `yen-telegram-pilot.service`,
`yen-dashboard-pilot.service`, `theoses2-telegram.service`, and
`theoses2-dashboard.service` are all active. Yen `/healthz` returns
`{"ok":true}`, and the public dashboard route responds with the login redirect
(`303`). No Theoses2 unit or release was changed.

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
