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

The implementation is in `internal/session`, `internal/agent`,
`internal/provider`, `internal/tools`, and `cmd/theoses`.

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

- `go test ./...`: 7 tests passed;
- `go test -race ./...`: 7 tests passed;
- `go vet ./...`: passed;
- local CLI build: passed;
- local SSE acceptance: CLI sent a prompt, received a fragmented tool call,
  read `README.md`, printed the final response, persisted four v3 entries,
  then a second process reopened the same file and appended a new turn.

The normalized TypeScript event trace is recorded in
[M1-GOLDEN-TRACE.md](M1-GOLDEN-TRACE.md). The Go event-order test covers the
successful tool path; the provider and CLI acceptance cover the wire and
user-visible path.

## Known differences

This slice does not claim parity for partial update events, provider retries,
usage accounting, malformed-stream recovery, live credentials, full read-path
recovery, images, complete truncation metadata, CLI error exit acceptance,
channel-independent shared sessions, semantic memory, episodic memory,
Telegram, dashboard, RPC, or deployment rollback.

Those remain explicit next milestones. TypeScript remains the operational
fallback.
