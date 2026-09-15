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

A side-by-side VPS pilot is now active without touching the existing
TypeScript units. The replacement Telegram token authenticates, the Go bot
has produced a live reply, a Go CLI turn has appended to the same canonical
conversation/session, and the shared-memory binary has restarted cleanly.
The Go dashboard also resolved to that conversation and completed live
requests, including shared memory, cancellation, and FIFO queue acceptance.
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
  links, open, append, and restart continuation.
  Opening a pinned TypeScript v1/v2 JSONL session now migrates it in place to
  v3: IDs and parent links are assigned, `hookMessage` becomes `custom`, and
  TypeScript flat compaction entries are read. Raw extension entries and
  message metadata survive migration; branch semantics and malformed-line
  recovery remain open. V2 sessions retain their existing tree links during
  the v2-to-v3 role/version migration.
  Assistant provider usage now round-trips through the session log.
  Compaction entries, active-context projection, and provider-backed
  `Runner.Compact` are supported. Set `THEOSES_AUTO_COMPACT_TURNS` to enable
  the pre-prompt automatic threshold for a deployment; overflow retry and
  `THEOSES_AUTO_COMPACT_OVERFLOW=1` to enable one bounded overflow
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
  and cookie login boundary when `THEOSES_DASHBOARD_TOKEN` is configured.
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
`THEOSES_AUTO_CONSOLIDATE=1`; it uses a separate consolidation-state file,
the pinned 70-message trigger ceiling, and a 15-minute failure cooldown so it
does not overwrite durable turn checkpoints. Its transcript is also capped at
100,000 characters from the tail, matching the pinned memory-consolidation
ceiling, and its provider call retries up to three times with exponential
2-second-base delays without changing ordinary agent-turn retry behavior.
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
