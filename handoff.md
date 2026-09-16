# Theoses2 Go rewrite handoff

Date: 2026-09-15

## Current state

The first Go vertical slice is implemented and locally accepted:

```text
CLI -p prompt
  -> v3 JSONL session
  -> agent loop
  -> OpenAI-compatible SSE provider
  -> read tool
  -> final CLI text
```

No cutover or TypeScript modification has happened. Local Telegram/dashboard
adapter code and acceptance fixtures exist; TypeScript remains the operational
fallback.

Current follow-up work is grouped in branch `feat/extension-registry`. The
in-process extension registry composes registered agent/tool/provider hooks,
keeps command and renderer registrations deterministic, and exposes extension
commands through RPC `get_commands`. It is covered by
`internal/extensions/registry_test.go` and the RPC command-discovery test.
The dynamic TypeScript extension loader, extension-file discovery, shortcuts,
flags, session actions, and provider-registration config are still deferred.

Latest code checkpoint: branch `feat/persistent-external-tools`, PR #45, commit
`f56f8b6`. It keeps external HTTP/MCP tools open across turns and closes them
when the Yen runtime exits, while turn-scoped built-ins remain unchanged.
Local verification reports 337 passing tests, including race, vet, and diff
checks. The PR is awaiting GitHub CI and review; no deployment was performed
for this slice.

Provider credentials are intentionally split: Yen must use the separate
`/etc/yen/yen-provider.env` file. Yen deployment configuration lives under
`/etc/yen` and `/var/lib/yen`; the existing Theoses provider env is not a Yen
deployment input.

The dashboard SSE seam now emits the TypeScript event names and payload shapes
for `delta`, `tool_call`, `tool_result`, `usage`, `done`, and `error`, including
the TypeScript stream headers. This is covered by the agent event callback test
and dashboard HTTP stream test; commit `aa7f816`.

Active session context now follows the last leaf through `parentId`, matching
the pinned TypeScript branch-path behavior. The v2 branch fixture proves that
an earlier sibling is excluded from the runtime context; the durable read-back
still retains both siblings.

Dashboard session GET now includes the TypeScript UI contract (`session`,
`history`, and `runtime`) while retaining the earlier `messages` field for
pilot clients. The response is covered by the dashboard HTTP test.

Dashboard session listing now maps registry links to the TypeScript session-view
shape (`id`, `channel`, `title`, `modified`, `messageCount`, `path`), includes
both dashboard and Telegram links, and de-duplicates shared conversation IDs.

The live Go pilot now runs release `eb73910` with Yen-owned configuration and
data paths. A real dashboard request succeeded through the separate Yen
provider key after the data migration; all four Go/TypeScript units were
verified active. Auto-consolidation was not enabled for this split-provider
check.

Auto-consolidation is now enabled through `YEN_AUTO_CONSOLIDATE=1` in
`/etc/yen/yen.env`. A live `thanks` trigger succeeded using Yen's separate
provider key; `/var/lib/yen/memory/consolidation-state.json` advanced and the
SQLite episodic store read back a new current record. This acceptance did not
modify or restart any TypeScript unit.

The provider wrapper now maps `YEN_REASONING_EFFORT` to the OpenRouter
`reasoning.effort` request field. The live Yen provider env sets it to `high`,
and release `39cb375` passed a real dashboard request after restart. The
request-shape contract is covered by `TestOpenAICompletionsSendsReasoningEffort`.

Telegram now sends the Bot API `typing` action immediately and every four
seconds while a turn runs, stopping it when the turn completes. Errors from
the indicator are intentionally non-fatal, matching the pinned TypeScript
behavior; `TestTelegramBotSendsTypingActionDuringTurn` covers the lifecycle.

Plain Telegram output now uses the pinned TypeScript 4,000-character limit;
`TestChunkTelegramTextMatchesTypeScriptLimit` covers the boundary.

Telegram message and quoted-reply handling now falls back from `text` to
`caption`, matching the pinned TypeScript extractor; the behavior is covered
by `TestTelegramBotUsesCaptionForMessageAndReply`.

Telegram stop controls now accept the pinned case-insensitive `stop`, `halt`,
`/stop`, and `/cancel` forms, retaining Yen's existing `/abort` alias.

Telegram replies now attempt the oracle's `sendRichMessage` Bot API method and
fall back to classic `sendMessage` on any unsupported or failed request.

Outbound replies also split on standalone `---` sections and thread each
following message to the prior sent message, matching the Telegram source
adapter's reply chain.

Telegram poll batches now dispatch concurrently while the runtime queue keeps
same-conversation turns FIFO; this lets stop/control updates reach an active
turn without serial poll-loop blocking.

Classic Telegram fallback now escapes and formats common Markdown constructs as
HTML before `sendMessage`; the rich path remains preferred.

Telegram now emits a `Running <tool>...` status message for tool calls,
covered by `TestTelegramBotReportsToolStatus`; canonical shared-session routing
is preserved through the event-enabled path.

For short single-section replies, the status message is edited in place into
the final answer, with rich-edit then classic-edit fallback.

Telegram supports the oracle's `/on tool call(s)` and `/off tool call(s)`
commands with bounded command/path/query previews. The preference persists in
Yen's data directory as `telegram-preferences.json`.

A side-by-side VPS pilot is now active without touching the existing
TypeScript units. The replacement Telegram token authenticates, the Go bot
has produced a live reply, a Go CLI turn has appended to the same canonical
conversation/session, and the shared-memory binary has restarted cleanly.
The Go dashboard also resolved to that conversation and completed live
requests, including shared memory, cancellation, and FIFO queue acceptance.

The Yen dashboard now has a Yen-owned token configured in `/etc/yen/yen.env`;
VPS acceptance returned 401 without credentials and 200 for Bearer, login
cookie, and health requests. Theoses2 dashboard configuration was untouched.

Telegram photos/documents and common media now download through bounded Bot API
calls into `/var/lib/yen/telegram-artifacts`; the prompt points the existing
read tool at the saved file. Photos also survive into OpenAI-compatible image
content parts; live attachment acceptance still requires an incoming user file.
Image MIME types and persisted image metadata now round-trip through sessions.
See [docs/M7-PILOT-READINESS.md](docs/M7-PILOT-READINESS.md),
[docs/M9-OPERATIONS.md](docs/M9-OPERATIONS.md), and
[docs/M10-PARITY-REPORT.md](docs/M10-PARITY-REPORT.md).

Canonical rewrite repository: `https://github.com/H4fizWasabie/yen`.

## Oracle

- Repository: `https://github.com/H4fizWasabie/theoses2`
- Behavioral source baseline: `3a910426a0db91570392c20c281ae8dfd82e01a1`
- Pinned reference: [docs/UPSTREAM.md](docs/UPSTREAM.md)

The sibling checkout currently points at `92f3855ef`, but it is clean. Use
`git show 3a910426...:<path>` or a temporary archive for source evidence. Do
not use the newer checkout as an unqualified oracle.

## What exists

- `internal/session`: v3 JSONL creation, deferred first publication, parent
  links, open, append, and restart continuation. Session open now scans past
  malformed/blank lines before the header and malformed entries after it, as
  covered by `TestOpenSessionSkipsMalformedLinesBeforeAndAfterHeader`.
  Scanner capacity is explicitly bounded at 4 MiB and covered by
  `TestOpenSessionReadsLargeJSONLMessageWithinBound`.
  Opening a pinned TypeScript v1/v2 JSONL session now migrates it in place to
  v3: IDs and parent links are assigned, `hookMessage` becomes `custom`, and
  TypeScript flat compaction entries are read. Raw extension entries and
  message metadata survive migration; branch semantics and malformed-line
  recovery remain open. V2 sessions retain their existing tree links during
  the v2-to-v3 role/version migration.
  Assistant provider usage now round-trips through the session log.
  Compaction entries, active-context projection, and provider-backed
  `Runner.Compact` are supported. Set `YEN_AUTO_COMPACT_TURNS` to enable
  the pre-prompt automatic threshold for a deployment; overflow retry and
  `YEN_AUTO_COMPACT_OVERFLOW=1` to enable one bounded overflow
  compact-and-retry attempt. Full TypeScript compaction settings remain
  deferred.
- `internal/agent`: tool-turn loop and normalized event collection.
  Assistant responses now carry provider/model metadata through the runtime
  into the durable session entry, matching the TypeScript session fields.
- `internal/provider`: one OpenAI-compatible SSE client with fragmented tool
  call argument assembly.
- `internal/tools`: read-only local file tool with offset/limit, basic
  truncation, and cancellation checks.
- `cmd/theoses`: local `-p` CLI adapter.
- `internal/runtime`: canonical queue runner with session persistence and
  post-persistence checkpoint ordering; submitted turns wait for FIFO
  availability instead of being rejected while a same-conversation turn runs.
  Active leases renew during long turns, and durable cancellation is observed
  across runner processes.
- `internal/adapters`: deterministic Telegram message and dashboard HTTP
  adapter seams sharing the canonical registry, runner, and memory engine.
  Dashboard API authentication now supports the TypeScript-compatible Bearer
  and cookie login boundary when `YEN_DASHBOARD_TOKEN` is configured.
  Telegram replies now split at the Bot API text limit without breaking
  Unicode runes, preserve capped quoted-message context, and target the
  originating Telegram message when replying.
- `cmd/theoses-dashboard`: local dashboard process with `/healthz`; a local
  start/readiness check has passed on `127.0.0.1:18789`.
- `cmd/theoses-telegram`: standard-library Bot API polling process with an
  owner-chat guard; live credentials are intentionally not used here.
- `docs/M0-INVESTIGATION.md`: approved M0 problem/scope/ledger.
- `docs/M1-CONTRACTS.md`: contracts, later shared-session/memory seam, and
  working acceptance thresholds.
- `docs/M1-GOLDEN-TRACE.md`: TypeScript event trace.
- `docs/M2-FIRST-SLICE.md`: implementation evidence and known differences.

## Verification already run

All currently pass:

```bash
go test ./...
go test -race ./...
go vet ./...
go build -o /tmp/theoses-go ./cmd/theoses
```

The fixed workload test also passes: 32 independent conversations, ten 4 KiB
turns, peak process RSS 65,656 KiB under the 160 MiB working threshold.

The accepted shared identity decision is now recorded in `CONTEXT.md`,
`docs/adr/0001-canonical-conversation-and-memory.md`, and
`docs/M1-CONTRACTS.md`. `internal/conversation` provides the durable adapter
registry and FIFO turn queue; new CLI sessions resolve a canonical conversation
ID and workspace metadata.

`internal/memory` provides scoped semantic Markdown nodes with YAML front
matter, episodic SQLite records, durable checkpoints, an engine that records
canonical turns, explicit additive migration helpers, and agent-facing
`remember`/`save_note` tools plus bounded session-scoped `recall_turns`.
The engine also applies extracted fact/edge/episode batches idempotently before
advancing a checkpoint, and exposes an explicit provider-backed `Consolidate`
path for structured JSON extraction.
Consolidation edge writes now enforce the TypeScript closed relation vocabulary
(`prefers`, `attributed_to`, `depends_on`, `located_at`, `requires`,
`supersedes`, `used_in`, `maintains`).
Opt-in runtime consolidation is now available with
`YEN_AUTO_CONSOLIDATE=1`; it uses a separate consolidation-state file,
the pinned 70-message trigger ceiling, and a 15-minute failure cooldown so it
does not overwrite durable turn checkpoints. Its transcript is also capped at
100,000 characters from the tail, matching the pinned memory-consolidation
ceiling, and its provider call retries up to three times with exponential
2-second-base delays without changing ordinary agent-turn retry behavior.
Assistant tool calls, tool results, bash executions, and branch/compaction
summaries are condensed to bounded transcript summaries matching the pinned
TypeScript formatter.
Length-limited assistant responses now refuse to execute potentially truncated
tool arguments and return the same re-issue guidance shape as the TypeScript
agent loop.
The agent callback also exposes `tool_execution_start` and
`tool_execution_end` lifecycle events with arguments, result, and error state.
Recoverable provider `length` stops now use the existing overflow opt-in to
compact once and retry once, matching the pinned TypeScript recovery path.
Persisted bash, custom, branch-summary, and compaction-summary messages are
converted into provider-compatible user context, including the bash exclusion
flag and TypeScript summary wrappers.
Independent multi-tool batches execute concurrently and retain provider/tool
result ordering in the persisted conversation.
Consolidation now carries session-entry timestamps into the transcript prefix.
Persisted tool errors are labeled `FAILED` in that transcript, matching the
TypeScript consolidation summary.
The dashboard now serves a minimal same-origin HTML shell at `/` for login,
session navigation, history, new sessions, messages, and stop; it intentionally
does not claim full TypeScript dashboard UI parity.
Its composer now consumes the existing SSE endpoint for incremental text and
tool progress before refreshing the persisted session.
Session-list metadata now reads the actual shared JSONL session for message
count and last-entry timestamp instead of placeholder values.
Visible dashboard sessions are sorted newest-first like the TypeScript dashboard.
Dashboard session titles now use the first user message, capped at 80 runes,
with the conversation ID as fallback.
Dashboard send/read-back now falls back to a Telegram registry link for a shared
conversation when no dashboard-specific link exists.
Dashboard message requests now preserve bounded `replyContext` using the same
quoted-context wrapper as Telegram and the pinned TypeScript session.
Consolidation timestamped messages now follow the active parent-linked branch,
excluding inactive sibling entries.
When the configured provider supports it, consolidation also requests the
OpenAI-compatible `response_format: {type: "json_object"}` wire mode; generic
test providers retain the prompt-only fallback.
Migration accepts semantic Markdown or the legacy JSONL memory file explicitly;
it is never automatic and leaves source stores intact.

Local SSE acceptance also passed: a CLI process received a tool call, read a
fixture README, printed the final response, persisted four session entries,
then a second process reopened the same file and appended another turn.

The same acceptance is now a repository test through the shared runner; it also
checks the episodic turn record and consolidation checkpoint after persistence.

The local parity hardening pass also covers Unicode/path recovery, read
truncation metadata, the pinned memory query matcher, provider read-tool
schema, cross-handle checkpoint merging, and remote active-turn cancellation.

This is not parity signoff. The implementation still differs from the
TypeScript oracle in the areas listed in `docs/M2-FIRST-SLICE.md`.

## Pick up next

The agent loop now has pre-loop and per-provider context hook seams:
`ToolHooks.BeforeAgentStart` runs once and `ToolHooks.Context` runs before each
provider call over cloned message context. Hooks can replace messages without
mutating durable agent history. The pinned oracle contracts are
`packages/coding-agent/src/core/extensions/types.ts:687-691,716-725` and
`packages/coding-agent/src/core/extensions/runner.ts:984-1013`; Go evidence is
`internal/agent/loop.go` and
`TestRunAppliesBeforeAgentStartHookOnce` /
`TestRunAppliesContextHookBeforeEveryProviderCall`. Full gates pass on the
context-hooks worktree. This is hook plumbing only; a dynamic TypeScript
extension loader, richer prompt/image event fields, and extension
command/renderer registries remain open.

The hook slice also carries `ToolHooks.ProviderResponse` through the provider
context. HTTP providers call it after receiving each response and before
consuming the body, with status and copied headers. Oracle evidence is
`packages/coding-agent/src/core/sdk.ts:426-435` and
`packages/coding-agent/src/core/extensions/types.ts:709-714`; the focused
helper test and full gates pass. Bedrock Converse now reads the SDK raw
response metadata and error response boundary too; the remaining gap is the
dynamic TypeScript extension loader and registries.
The Bedrock success and error extraction paths are covered by
`TestBedrockResponseHookReadsSDKRawResponseMetadata` and
`TestBedrockErrorResponseHookReadsSDKResponseError`.

The provider, read, CLI error-boundary, fixed-capacity, canonical identity, and
queue slices are now implemented and locally verified. Work in this order:

1. Compare Go and TypeScript normalized traces and session read-backs. Update
   the parity ledger only with TypeScript source evidence, Go tests, and a
   golden/local acceptance result.
2. Compare the new memory stores against more TypeScript edge/search cases and
   record any accepted parity differences. The semantic edge walk and episodic
   search/point-in-time paths now have Go coverage.
3. Add adapter-facing migration/read-back integration around the existing
   deterministic seams; keep startup memory import disabled. Session v1/v2
   read-back migration now exists; the explicit semantic/episodic migration
   command remains additive and source-preserving.
4. Validate adapter reconnect/read-back behavior under concurrent processes;
   the queue and registry now use cross-process file locking, expiring
   per-turn leases, durable cancellation, and a subprocess claim test.
5. Retry the Telegram pilot only with a valid replacement token; then verify
   visible delivery, restart/resume, shared memory, and rollback before any
   cutover decision.

6. Exercise the configured dashboard token through `/api/login`, Bearer and
   cookie requests in the side-by-side pilot before claiming live dashboard
   authentication parity.

7. Run `deploy/install-side-by-side.sh` on a disposable host with prepared
   channel/provider env files, keeping `YEN_START=0` for the first read-back;
   do not use the production VPS as the installer test host. The isolated
   user-namespace installer acceptance has already passed with both binaries,
   wrappers, units, mode `600` channel env, and no service start.

The dashboard token boundary has also passed isolated local acceptance: health
200, unauthenticated API 401, `/api/login` 200, cookie 200, and Bearer 200.
The 2026-09-15 VPS read-back found both Go pilot units and both existing
TypeScript units active, but no dashboard token configured in the Go env files;
therefore live authenticated dashboard acceptance remains intentionally open.
The installer acceptance was rerun after the consolidation wrapper change:
temporary root/fake systemctl, both binaries and wrappers, service units,
mode-600 channel env, and no service start all passed.

## Latest pickup: bounded legacy semantic migration

Explicit semantic-memory migration now uses the same 4 MiB JSONL line bound as
session import, so large historical records are not rejected by Scanner's
default 64 KiB limit. `internal/memory/migrate_test.go` covers a large record;
focused memory tests and the full test, race, vet, and diff gates pass.

## Latest pickup: RPC bash persistence

On 2026-09-15, direct JSONL RPC `bash` now persists a TypeScript-compatible
`bashExecution` session message containing the command, output, and
`excludeFromContext` flag, in addition to the existing automatic Working Note
entry. `internal/rpc/server_test.go` covers the durable record. The change is
ready to commit and push with the open GitHub PR before selecting the next gap.

The provider registry was then expanded to the pinned OpenAI-compatible
provider aliases (including Ant Ling, Baseten, Cerebras, Fireworks, Hugging
Face, Kimi, Moonshot CN, NVIDIA, Qwen token plans, Together, Xiaomi token
plans, and Z.AI CN), each with Yen-owned credential variables and table-driven
configuration tests. This is configuration/API-shape coverage only; providers
with distinct protocols, OAuth, or provider-specific model catalogs remain
open.

The next provider increment adds native Google Gemini REST streaming in
`internal/provider/google.go`: text, thinking parts, function calls, usage,
stop reasons, data-URI images, retries, and `YEN_GOOGLE_API_KEY` configuration.
`internal/provider/google_test.go` covers the wire shape and event/result
mapping. The full gate is now 233 tests plus race/vet/diff; commit and push this
work before moving to the next provider or channel gap.

The Gemini adapter also implements the native `/models` catalog and filters to
models advertising `generateContent`; the current full gate remains 234 tests
plus race/vet/diff.

The dashboard history projection and embedded shell now also render durable
`bashExecution` records, including the command, output, and excluded-context
marker. Focused dashboard tests pass; the full gate is now 254 tests plus
race/vet/diff.

Native OpenAI Responses support is now in `internal/provider/responses.go`.
It covers `/responses` SSE events, text, incremental function-call arguments,
terminal response usage/status, image input, retries, and
`openai-responses`/`azure-openai-responses` Yen configuration. The pinned
source evidence is `packages/ai/src/api/openai-responses-shared.ts:597-750`,
and `internal/provider/responses_test.go` covers the wire/result contract. The
full gate is 236 tests plus race/vet/diff; commit and push this work before the
next gap.

The interactive CLI now also exposes `/model`, `/thinking`, `/retry`, and
`/trust`, backed by the same provider/settings helpers used by RPC. Tests cover
the mutations and the trust-file override; the full gate is 239 tests plus
race/vet/diff. Full TUI selectors, OAuth commands, and session-picker flows
remain open.

CLI inspection now also exposes `/tree` and `/artifacts`, backed by the
durable session tree and artifact catalog. Full TUI rendering, selectors, and
session-picker flows remain open.

The authorized side-by-side VPS deployment now runs `/opt/yen/releases/0f97e38`.
`yen-telegram-pilot.service`, `yen-dashboard-pilot.service`,
`theoses2-telegram.service`, and `theoses2-dashboard.service` all read back
`active`; Yen `/healthz` returns `{"ok":true}`. The Theoses2 services were not
restarted or modified.

The 2026-09-15 authenticated read-only VPS check confirmed the shared-channel
identity in `/var/lib/yen/conversations.jsonl`: Telegram, CLI, and dashboard
links all point to `conv-5955b65fd7cbaf4f869df7af7f6c1fb6`, and dashboard
`/api/sessions` returns one deduplicated shared session with 56 messages. A
new cross-channel prompt/replay acceptance is still required before claiming
full live session parity.

That acceptance was completed on 2026-09-15 through the authenticated Yen
dashboard: POSTing the deterministic prompt returned SSE `delta`, `usage`, and
`done`, with `CROSS_CHANNEL_OK` in the delta; the following GET of the same
canonical conversation read back the assistant result. This proves dashboard
write/read continuity; an incoming Telegram replay is still open.

Yen now has `internal/auth`, a mode-0600 atomic JSON credential store with
serialized per-process mutation, secret-free listing metadata, API-key/OAuth
record shapes, and explicit `YEN_AUTH_FILE` fallback wiring for provider
configuration. This is the storage/auth boundary only; provider-specific
OAuth login, refresh, and browser/device flows remain open. The full gate is
239 tests plus race/vet/diff.

External tool lifecycle is now wired through `codingagent.CloseTools` and a
runtime defer: MCP stdio tools expose cleanup, failed stdio initialization
closes its child, and deferred tools close their underlying resources. The
cleanup seam has a regression test; the full gate is now 240 tests plus
race/vet/diff.

## Latest pickup: interactive CLI bash

The interactive CLI now ports the TypeScript `!command` and `!!command` path:
it executes in the session workspace, prints output, records a durable
`bashExecution` message, and marks `!!` output excluded from model context.
`cmd/theoses/main_test.go` covers the excluded form. The full gate is now 241
tests; run the complete race/vet gate before the next parity slice.

It also supports `/export [path.jsonl]` and `/import <path.jsonl>` using the
existing validated session copy primitive; import reloads the active session
from disk. The focused test covers both directions. The full gate is now 242
tests plus race/vet/diff. Clone/new and the full TUI selector flow still need
an active-session ownership slice.

`/clone [path.jsonl]` now creates a durable fork at the current leaf, preserves
the source path as `parentSession`, and switches the active CLI session and
runner path to the clone. `/new` creates and switches to a fresh session while
preserving the canonical conversation identity. Focused clone/new tests and
the full 244-test gate pass.

## Latest pickup: credential-store process locking

The Yen-owned auth store now uses an OS file lock around every read and the
entire read-modify-write mutation, so separate Yen processes sharing
`YEN_AUTH_FILE` cannot overwrite each other. A concurrent two-store regression
test passes; the full gate is now 245 tests plus race/vet/diff. The lock is
per-file by design; split per-provider only if contention is measured.

## Latest pickup: MiniMax provider mappings

The provider registry now includes `minimax` and `minimax-cn`, using the
pinned native Anthropic Messages endpoints and Yen-owned API-key variables.
`AnthropicMessages.ProviderName` preserves the provider identity for model
controls and diagnostics. Configuration tests cover both mappings; the full
gate is now 248 tests plus race/vet/diff.

Vercel AI Gateway is now mapped to the native Anthropic Messages client with
`YEN_VERCEL_AI_GATEWAY_API_KEY`; the side-by-side installer passes this key,
plus the MiniMax keys, through its isolated service environment. Configuration
and shell syntax checks pass; the full gate is now 249 tests plus race/vet/diff.

The shared provider client now emits Mistral's native `reasoning_effort` field
when `ProviderName` is `mistral`, while preserving the existing `reasoning`
shape for other OpenAI-compatible providers. A wire regression test covers the
distinction; the full gate is now 250 tests plus race/vet/diff.

The interactive CLI also exposes `/settings` as JSON settings read-back and
`/reload` to reopen the active session from disk. Focused command coverage and
the full 251-test race/vet gate pass.

The dashboard history projection and embedded browser shell now preserve and
render persisted assistant `thinking` segments instead of treating them as
generic tools. Focused API/shell tests pass; the full gate is now 253 tests
plus race/vet/diff.

`convert_doc` now bounds markitdown stdout at the oracle's 2,000,000-byte
`maxBuffer` using a capped pipe reader, and terminates an oversized producer.
`internal/codingagent/convert_doc_test.go` covers the ceiling; the full gate
is now 255 tests plus race/vet/diff.

`web_search` now snapshots Yen's filtered Tavily key list when the tool is
constructed, matching the TypeScript tool-definition boundary while retaining
legacy test construction that leaves keys unset. Fallback and snapshot tests
pass; the full gate is now 257 tests plus race/vet/diff.

Context discovery now includes the oracle's `AGENTS.MD` and `CLAUDE.md`
resource names in addition to Yen's existing guidance files. The focused
resource test and full 258-test gate pass.

Dashboard API authentication now fails closed when `YEN_DASHBOARD_TOKEN` is
unset, returning the oracle-compatible 503 response. An integration test
covers the unconfigured-token case; the full gate is now 259 tests plus
race/vet/diff.

The read-only explorer now enforces its oracle turn ceilings in the harness:
8 turns for `quick-scan` and 15 for `deep-map`, with an explicit incomplete
answer when the ceiling is reached. Its prompt also requires the oracle's
distilled-answer and budget-footer contract; the full gate is now 260 tests.

The side-by-side installer now forwards every configured Yen provider key,
including the previously omitted Ant Ling, Baseten, Cerebras, Fireworks, Hugging
Face, Qwen, Together, Xiaomi, Z.AI-CN, Google, Azure, and auth-store variables
through both service wrappers. Shell syntax and the full Go gate pass.

GitHub Actions now runs the repository test, race, vet, and diff gates on
pushes and pull requests. This closes the prior PR-review gap where GitHub
reported no checks.

Native Google, Anthropic, MiniMax, and Vercel provider construction now
preserves `YEN_REASONING_EFFORT`, matching the configured provider path used
by the OpenAI and Responses clients. Provider tests cover Google and
Anthropic; the full gate is now 261 tests.

## Latest parity evidence

The coding-agent bash boundary now follows the oracle's separate execution
record contract: each session bash invocation appends a bounded working-note
command log and a durable `bashExecution` message carrying the full command,
output, exit code, cancellation, truncation, and `excludeFromContext` fields.
The implementation is in `internal/tools/bash.go`,
`internal/codingagent/bash.go`, and `internal/session/session.go`; focused
success/failure coverage is in `internal/codingagent/working_note_test.go`.
The full test, race, vet, and diff gates pass (261 tests).

Provider configuration also now recognizes the oracle's `opencode` and
`opencode-go` IDs with Yen-owned `YEN_OPENCODE_API_KEY` credentials and their
pinned OpenAI-compatible endpoints. Focused provider tests and the full gate
pass (263 tests). The model-specific API/catalog metadata is still not claimed
as complete parity.

Bash capture now keeps only a bounded 12 KiB tail in memory and spills output
larger than 6 KiB to a mode-600 temporary file, while preserving the path in
the durable `bashExecution` record. Large-output coverage passes and the full
gate is now 264 tests plus race/vet/diff.

Anthropic streaming now preserves cache-read/cache-write input-token usage and
computes total tokens from the stream, matching the oracle's message-start and
message-delta handling. Focused provider coverage and the full 264-test
race/vet/diff gate pass.

RPC `get_entries` now matches the oracle's `leafId` response and explicit
unknown-`since` error. Focused protocol coverage and the full 265-test
race/vet/diff gate pass.

RPC `get_commands` now advertises trusted local skills as `skill:<name>` with
description and source-path metadata. Focused command-discovery coverage and
the full 265-test race/vet/diff gate pass; executing extension, prompt, and
skill commands remains a separate open surface.

Prompt templates now load from project/global/configured directories, expand
`/name args` before the agent call, and appear in RPC discovery with prompt
source metadata. Focused expansion/discovery tests and the full 267-test
race/vet/diff gate pass.

Skill commands now expand `/skill:name args` into an XML skill block with the
skill body, location, relative-reference guidance, and trailing arguments.
Focused expansion coverage and the full 268-test race/vet/diff gate pass.

Prompt templates now support quoted arguments, multi-digit positional
placeholders, defaults, and argument slices in the same substitution pass as
the oracle. Focused template tests and the full 269-test race/vet/diff gate
pass.

Skill discovery and explicit skill expansion now use the containing directory
name when `SKILL.md` omits frontmatter `name`, matching the oracle fallback.
Focused resource coverage and the full 270-test race/vet/diff gate pass.

## Latest operations evidence

The current VPS data was backed up on 2026-09-15 to
`/var/backups/yen-20260915T093942Z.tgz`. Archive listing validation succeeded;
the archive is mode `600`, and all Yen and Theoses2 units plus Yen health
remained green. No runtime data was changed.

## Important deferred product change

The requested later direction is that CLI, Telegram, dashboard, and future
channels share one canonical session/conversation and the same semantic and
episodic memories. The TypeScript baseline does not do this: it keys sessions
by `(channel, channelSessionId)`. Do not merge by cwd or add channel adapters
until canonical identity, queue ownership, old-session migration, memory
scope, and checkpoint ordering are explicitly decided.

## Working rules

- Keep the full Theo Bible upstream; do not copy it into this repo.
- Do not modify the original repository.
- Do not claim parity from documentation alone.
- Keep changes narrow and test-first.
- Do not add all providers, dashboard, extensions, RPC, Telegram, or memory
  simultaneously.
- Do not deploy until rollback validation and explicit authorization exist.
