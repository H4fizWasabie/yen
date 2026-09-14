# M0 investigation: Theoses2 Go rewrite

Status: M0 approved for contract extraction; production Go implementation is
still not started.

## Baseline and authority

- Oracle repository: `H4fizWasabie/theoses2`
- Oracle commit: `3a910426a0db91570392c20c281ae8dfd82e01a1`
- Go workspace: this repository at `/home/hafiz/Desktop/yen`
- Deployment: not inspected or changed
- Original repository: not modified

The requested Bible files were read from the original checkout. Its README
identifies revision `e66120ddf`, while the requested behavioral baseline is
`3a910426a0db91570392c20c281ae8dfd82e01a1`; the rewrite plan is also a
working-tree addition there. That makes the pinned TypeScript source the only
safe behavior authority for this report.

One concrete example of the authority rule: the baseline source's
`limitActiveContextMessages` defaults to `maxTurns = 3`
(`packages/coding-agent/src/core/session-manager.ts:468-482`), despite nearby
comments/documentation referring to five turns in newer material. The first
slice must follow the pinned source until a later source revision is explicitly
adopted.

## 1. Concrete problem Go is expected to solve

The approved problem is operational capacity across three related dimensions:

- establish and enforce a resident-memory ceiling under representative agent
  workloads;
- support concurrent independent conversations without cross-session state
  corruption, while serializing turns that share one canonical conversation;
- produce a simpler, repeatable deployment and rollback unit than the current
  multi-package TypeScript runtime.

The exact ceiling, concurrency level, and deployment SLO are still to be
measured during M1. No performance or reliability benefit is claimed yet. The
same workload, prompts, provider behavior, visible output, persisted writes,
and restart/rollback checks must run against TypeScript and Go. If Go cannot
improve an agreed metric without compatibility loss, stop the rewrite.

## 2. Proposed first vertical slice

This is the approved M0 slice boundary. It is not production implementation
authorization until the M1 contracts and measurements pass their exit gate.

**CLI single-shot text mode:**

1. Start in a working directory with `-p "Read README.md and summarize it"`.
2. Create or resume the `cli` session for that cwd.
3. Send one user message through the agent loop.
4. Allow the configured model to call only the local `read` tool.
5. Feed the tool result into the next provider turn.
6. Persist the user, assistant/tool, model/thinking, and operation entries in
   the compatible session log.
7. Print the final assistant text and return a non-zero exit code for an
   assistant/provider error.

Recommended provider boundary: one configured `openai-completions` model. The
existing faux provider is for deterministic contract tests only; it is not
evidence of live provider parity. The exact model ID, endpoint configuration,
credential source, timeout, and retry settings remain unknown until the M0
problem and acceptance workload are chosen.

Recommended required tool set: `read` only. It is enough to prove the
assistant-tool-assistant loop while preserving a safe, non-mutating user
journey. `bash`, `write`, `edit`, memory tools, extensions, and image/document
paths are outside this first slice.

## 3. Primary user journey and channel

Primary channel: local CLI `-p`/print text mode.

The user asks a repository question, the model reads a file, and the user gets
one final text response. This channel is the smallest real adapter with a
visible output, a persisted session, and no Telegram/network-delivery control
plane.

The source path is `packages/coding-agent/src/modes/print-mode.ts:33-168`:
it binds to an `AgentSessionRuntime`, subscribes to events, prompts the
session, prints assistant text, maps assistant errors to exit code 1, and
disposes the runtime. `packages/coding-agent/src/main.ts:725-821` constructs
the services/session and `:834-840` constructs the runtime used by that mode.

Telegram is deferred. Its source adds owner credentials, channel-keyed session
lookup, per-chat queues, stop state, status-message mutation, reply context,
images, artifacts, tool-call rendering, and Telegram delivery
(`packages/telegram/src/index.ts:340-385`, `:393-415`, `:630-700`). It is a
second adapter contract, not a cheap first channel.

## 4. Compatibility policy for the slice

### Sessions

- Read and write the baseline v3 JSONL shape for the in-scope CLI entries.
- Preserve the session header's `id`, timestamp, cwd, `channel: "cli"`, and
  cwd-derived `channelSessionId` (`session-manager.ts:48-64`, `:1022-1048`).
- Preserve append-only entry IDs and `parentId` links.
- Support create, open, and continue-recent for the selected cwd
  (`session-manager.ts:1815-1900`).
- Preserve in-scope message persistence and the baseline's lazy flush behavior
  (`session-manager.ts:1279-1329`): no assistant message means pending entries
  are not yet published as a complete persisted conversation.
- Do not claim compatibility for branch navigation, compaction, migration,
  artifacts, labels, or arbitrary historical entry types in this slice.

### Memory

The first slice does not implement semantic graph memory, episodic SQLite
memory, `remember`, `save_note`, `recall_turns`, Working Note, consolidation, or
compaction. Existing memory data must not be rewritten or silently imported.

The later approved product direction is that CLI, Telegram, dashboard, and
future channels share one canonical conversation/session and the same semantic
and episodic memory. This is not the current TypeScript behavior: the baseline
session lookup tuple is `(channel, channelSessionId)`
(`session-manager.ts:709-717`, `:1973-1993`), Telegram uses the chat as its
channel session ID (`telegram/src/index.ts:343-347`), and dashboard creates its
own channel/id pair (`dashboard/src/index.ts:299-306`). M1 must extract the
canonical identity and queue contract before any Go memory or multi-channel
implementation.

### APIs and protocols

- The only user-facing contract is local CLI text output and exit status.
- The provider request/stream contract is limited to the selected
  `openai-completions` model.
- No RPC JSONL, CBOR framing, Unix socket, client/server protocol, dashboard
  API, Telegram API, or wire-protocol compatibility claim is made.
- No Go package API is considered stable until M1 contracts are reviewed.

### Tools and trust

Expose only `read`. Preserve its relative/absolute path handling, cwd boundary,
abort behavior, line/byte truncation, and safe path recovery as defined by
`packages/coding-agent/src/core/tools/read.ts:21-25`, `:209-245`, and
`:362-363`. Do not add mutation or external-content tools to make the demo
look broader.

## 5. TypeScript source, symbols, callers, and tests

### Runtime path

| Concern | Baseline authority and callers |
|---|---|
| CLI construction | `packages/coding-agent/src/main.ts:725-821`: `createAgentSessionServices` then `createAgentSessionFromServices`; `:834-840`: `createAgentSessionRuntime`. |
| Service ownership | `packages/coding-agent/src/core/agent-session-services.ts:137-223`: `createAgentSessionServices`, `createAgentSessionFromServices`. |
| Session construction | `packages/coding-agent/src/core/sdk.ts:225-479`: `createAgentSession`; resolves model/auth, restores context, constructs `Agent`, installs default tools, and creates `AgentSession`. Its provider callback delegates to `modelRuntime.streamSimple` at `:388-448`. |
| Session state/persistence | `packages/coding-agent/src/core/session-manager.ts:48-203` (`SessionHeader`, entry types); `:441-545` (`sessionEntryToContextMessages`, `buildSessionContext`); `:1022-1049` (new session); `:1279-1357` (append/persist); `:1815-1900` (create/open/continueRecent). |
| Product prompt state machine | `packages/coding-agent/src/core/agent-session.ts:1140-1154` (`_runAgentPrompt`); `:1200-1362` (`prompt`); `:662-756` (`_handleAgentEvent`, persistence); `:890-930` (subscription/disposal); `:1639-1650` (abort/idle). |
| Low-level agent API | `packages/agent/src/agent.ts:173-238` (`Agent`); `:250-253` (`subscribe`); `:318-388` (`abort`, `waitForIdle`, `prompt`, `continue`). |
| Agent loop | `packages/agent/src/agent-loop.ts:95-143` (`runAgentLoop`, continuation); `:155-275` (`runLoop`, tool turns, steering/follow-up, event order); `:281-371` (streaming and final message events). |
| Provider dispatch | `packages/ai/src/models.ts:136-148` (`Provider.streamSimple`); `:690-703` (`Models.streamSimple`); `packages/ai/src/api/openai-completions.ts:699-717` (`streamSimple`) and `:281-330` (stream request/error setup). |
| Required tool | `packages/coding-agent/src/core/tools/read.ts:21-25` (schema); `:209-245` (`createReadToolDefinition` and execution); `:362-363` (`createReadTool`). |
| Channel output | `packages/coding-agent/src/modes/print-mode.ts:33-168` (`runPrintMode`). |

### Existing tests that define the slice

- `packages/agent/test/agent-loop.test.ts:118+`: event order, context
  conversion, tool execution, and continuation.
- `packages/agent/test/agent.test.ts:160+`, `:263+`: lifecycle events,
  abort signal delivery, abort, and queued behavior.
- `packages/agent/test/e2e.test.ts:62-159`: faux-provider prompt/tool loop,
  abort, lifecycle, and state assertions.
- `packages/coding-agent/test/suite/agent-session-prompt.test.ts:30-102`:
  idle prompt persistence and a tool-call turn followed by an assistant turn;
  later cases cover image and prompt behavior but are deferred here.
- `packages/coding-agent/test/suite/agent-session-queue.test.ts`: steering and
  follow-up queue semantics; useful contract evidence, but queueing is not a
  first-slice user feature.
- `packages/coding-agent/test/session-manager/build-context.test.ts`:
  context projection and active-context behavior.
- `packages/coding-agent/test/session-manager/custom-session-id.test.ts`,
  `migration.test.ts`, `save-entry.test.ts`, and
  `sdk-session-manager.test.ts`: session IDs, persisted headers/tree entries,
  migration behavior, custom-entry exclusion, and SDK session paths.
- `packages/coding-agent/test/print-mode.test.ts:93-141`: print-mode prompt
  wiring, text/JSON mode cleanup, and assistant-error exit behavior. Only the
  text-mode contract is selected initially.
- `packages/coding-agent/test/block-images.test.ts:73+` and
  `audit-file-safety.test.ts:1+`: direct read-tool behavior and path recovery
  safety. Image handling remains deferred even where the test file covers it.
- `packages/ai/test/faux-provider.test.ts:30-180`: deterministic stream,
  tool-call, usage, response-order, and error fixtures.
- `packages/ai/test/provider-retry.test.ts` and the relevant
  `openai-completions-*` tests: provider retry and request-shape evidence to
  select only after the exact live model is chosen.

These tests are source evidence and characterization targets, not Go parity
results. No Go tests or golden traces exist yet.

## 6. Explicit non-goals and deferred features

- Full TypeScript-to-Go rewrite.
- All providers, model catalog generation, OAuth, and provider-specific
  extensions.
- Telegram, dashboard, browser UI, RPC, CBOR, client/server, Unix transport,
  and protocol compatibility.
- Bash, write, edit, find, grep, web search, image generation, document
  conversion, sidecars, MCP, and extension tools.
- Semantic/episodic memory, Working Note, compaction, consolidation, artifacts,
  branches, forks, labels, and session migrations.
- Multi-user ownership, authentication, deployment, systemd, VPS rollout,
  cutover, or decommissioning.
- Production Go implementation before the M1 contracts and measurements pass
  their exit gate.

## 7. First parity-ledger entries

Status vocabulary follows the rewrite plan. `unknown` means TypeScript is
understood enough to test, but Go evidence does not exist.

| ID | Capability | TypeScript authority | Test/trace | Go status | Difference | Decision |
|---|---|---|---|---|---|---|
| P-001 | CLI session create/resume keyed by cwd | `session-manager.ts:1022-1049`, `:1815-1900` | TS session tests; Go session tests; two-process local CLI acceptance | partial | default path/ID discovery and historical session migration are not ported | partial |
| P-002 | v3 header, parent links, append-only JSONL, lazy flush | `session-manager.ts:48-71`, `:1279-1329` | TS session tests; Go JSONL test; acceptance read-back | partial | current Go entry model is limited and not migration-compatible | partial |
| P-003 | Prompt lifecycle and event ordering | `agent.ts:250-388`, `agent-loop.ts:95-275` | TS golden trace; Go event-order test; local acceptance | partial | Go currently aggregates provider text and lacks partial update events | partial |
| P-004 | Assistant tool call -> `read` -> tool result -> next assistant turn | `agent-loop.ts:202-224`, `read.ts:209-245` | TS trace; Go loop/provider/read tests; local SSE acceptance | partial | read path recovery and full truncation details remain incomplete | partial |
| P-005 | One `openai-completions` streamed response and provider error | `models.ts:690-703`, `openai-completions.ts:699-717` | TS provider tests; Go SSE/tool-delta tests; local SSE acceptance | partial | no live provider, retry policy, usage, or malformed-stream parity | partial |
| P-006 | CLI final text and error exit status | `print-mode.ts:139-161` | TS print tests; Go build; local CLI success acceptance | partial | CLI error acceptance and output formatting are incomplete | partial |
| P-007 | Read-tool path, truncation, and abort safety | `read.ts:21-25`, `:209-245` | TS path tests; Go read test; local tool acceptance | partial | scope deliberately narrower than full read tool | partial |
| P-008 | Telegram delivery and channel-keyed session behavior | `telegram/src/index.ts:340-385`, `:630-700` | Telegram integration/live acceptance | not started | outside first slice | deferred |
| P-009 | Future shared canonical conversation across CLI, Telegram, dashboard, and other channels | Current channel-keyed source above; desired change requires a new reviewed contract | Cross-channel session identity, ordering, and replay trace | not started | intentional product change, not baseline parity | deferred |
| P-010 | Future shared semantic and episodic memory across channels | `memory-store.ts:200+`, `episodic-store.ts:53-163`, `memory-consolidation.ts:535-578` | Cross-channel consolidation and recall fixture with restart | not started | current checkpoints are channel-session keyed | deferred |

No row is accepted as parity until it has TypeScript source evidence, a Go
test, and a normalized golden trace or live acceptance result where relevant.

## 8. Unknowns, risks, and stop conditions

### Unknowns and risks

- The exact memory ceiling, concurrency target, and deployment SLO still need
  baseline measurement and explicit acceptance thresholds.
- The Bible and source baseline are different revisions; later documentation
  must not silently override `3a910426a`.
- The exact provider, model ID, auth source, endpoint, retry policy, and live
  acceptance environment are not chosen.
- JSONL persistence has lazy publication and tree semantics; a simpler flat
  log would be behavior drift, not a harmless implementation detail.
- Provider streaming includes partial-message replacement, terminal events,
  usage, tool-call IDs, abort, and error stop reasons. Final text alone is
  insufficient evidence.
- The baseline has a three-user-turn context cap despite conflicting nearby
  wording. That value must be fixture-tested before porting.
- `read` has path recovery and truncation edge cases; a direct `os.ReadFile`
  replacement would not prove compatibility.
- A real channel acceptance requires credentials and observable process/output
  read-back. No live provider or channel result is claimed.

### Stop conditions

Stop M0 and request a decision if the Go problem cannot be stated as a metric.
After approval, stop the slice if any of these occur:

1. Existing baseline v3 sessions cannot be replayed without loss or an
   explicitly accepted migration.
2. Event order, tool-call identity, streaming termination, abort, or error
   semantics differ in a golden trace.
3. `read` permits a path/truncation behavior the TypeScript tests reject.
4. The chosen provider works only in a happy-path demo; retry, malformed
   stream, usage, and abort behavior remain untested.
5. The CLI output/exit contract or persisted session read-back differs.
6. The measured Go target is not improved under the agreed workload.
7. A live pilot would require deployment or irreversible cutover before
   rollback validation exists.

## M0 decision

M0 is approved for contract extraction. Production Go remains gated on the M1
contract, baseline-measurement, and acceptance-threshold exit gate. This
workspace contains no production Go code, no copied Bible, and no deployment;
the TypeScript oracle remains unchanged.
