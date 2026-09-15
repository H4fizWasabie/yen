# First Go slice: CLI, session, loop, provider, read

Status: locally accepted against a deterministic SSE fixture; not parity
signoff and not deployed.

## Scope implemented

- v3 JSONL session creation, deferred first publication, parent links, open,
  and append-after-restart;
- one agent loop with assistant tool calls and tool-result continuation;
- one OpenAI-compatible streaming SSE provider;
- one read-only local `read` tool with relative paths, offset/limit, basic
  line/byte truncation, and context cancellation;
- one local CLI `-p` adapter.

The provider also rejects a stream that ends without a `finish_reason`,
matching the pinned OpenAI-compatible stream contract, and records streamed
usage totals with bounded retries. `read` supports `@` path recovery and
reports an overlong first line instead of returning an empty result.

Provider failures produce an assistant error/abort boundary, and streamed text
deltas reach the agent as `message_update` events. CLI failures return status 1
and persist the interrupted-turn boundary.

The implementation is in `internal/session`, `internal/agent`,
`internal/provider`, `internal/tools`, `internal/conversation`,
`internal/runtime`, `internal/memory`, `internal/adapters`, and `cmd/theoses`.

The approved memory seam is also implemented in `internal/memory`: semantic
Markdown nodes retain the TypeScript YAML front matter with explicit engine,
owner, workspace, and conversation scope; episodic records use SQLite with
conversation provenance; checkpoints use atomic JSON replacement. The runner
exposes scoped `remember` and `save_note` tools plus bounded session-scoped
`recall_turns` alongside `read`. Migration is explicit, idempotent, additive,
and never runs at startup. The engine has an explicit idempotent
fact/edge/episode application seam; model-driven consolidation remains an
explicit caller rather than an automatic extra provider turn.

## Evidence

TypeScript authority:

- session lifecycle/persistence: `packages/coding-agent/src/core/session-manager.ts:48-71`,
  `:1022-1049`, `:1279-1329`, `:1815-1900`;
- agent loop/tool order: `packages/agent/src/agent-loop.ts:155-275`, `:281-371`;
- provider stream: `packages/ai/src/models.ts:690-703`,
  `packages/ai/src/api/openai-completions.ts:699-717`;
- CLI output: `packages/coding-agent/src/modes/print-mode.ts:33-168`;
- read tool: `packages/coding-agent/src/core/tools/read.ts:209-345`.

Go evidence:

- `go test ./...`: 69 tests passed;
- `go test -race ./...`: 69 tests passed;
- `go vet ./...`: passed;
- fixed capacity test: 32 independent conversations, ten 4 KiB turns, all
  settled with no cross-talk; process peak RSS 65,656 KiB under the 160 MiB
  working threshold;
- local CLI build: passed;
- local dashboard process: started successfully and returned HTTP 200 from
  `/healthz` on `127.0.0.1:18789`;
- local SSE acceptance: CLI sent a prompt, received a fragmented tool call,
  read `README.md`, printed the final response, persisted four v3 entries,
  then a second process reopened the same file and appended a new turn.

The normalized TypeScript event trace is recorded in
[M1-GOLDEN-TRACE.md](M1-GOLDEN-TRACE.md). The Go event-order test covers the
successful tool path plus normalized success, tool, error, and abort outcomes;
the provider and CLI acceptance cover the wire and user-visible path.

## Known differences

This slice does not claim parity for live credentials, full read-path
recovery, or images,
RPC or deployment rollback. Telegram and dashboard adapter seams now have
local deterministic coverage, including dashboard SSE and local `/healthz`;
live Telegram delivery, auth, and UI acceptance remain.

Those remain explicit next milestones. TypeScript remains the operational
fallback.
