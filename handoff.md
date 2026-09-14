# Theoses2 Go rewrite handoff

Date: 2026-09-14

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

No deployment, cutover, Telegram/dashboard work, or TypeScript modification
has happened. TypeScript remains the operational fallback.

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
- `internal/agent`: tool-turn loop and normalized event collection.
- `internal/provider`: one OpenAI-compatible SSE client with fragmented tool
  call argument assembly.
- `internal/tools`: read-only local file tool with offset/limit, basic
  truncation, and cancellation checks.
- `cmd/theoses`: local `-p` CLI adapter.
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

Local SSE acceptance also passed: a CLI process received a tool call, read a
fixture README, printed the final response, persisted four session entries,
then a second process reopened the same file and appended another turn.

This is not parity signoff. The implementation still differs from the
TypeScript oracle in the areas listed in `docs/M2-FIRST-SLICE.md`.

## Pick up next

Work in this order:

1. Add Go tests for provider error, abort, malformed SSE, retry, usage, and
   partial update events; then implement only the behavior required by those
   tests.
2. Harden `read` against the pinned TypeScript cases: path recovery,
   symlink/traversal behavior, exact 500-line/12 KiB truncation, Unicode names,
   missing files, and abort-before-completion.
3. Add CLI error/exit-status acceptance and verify persisted interrupted-turn
   boundaries.
4. Compare Go and TypeScript normalized traces and session read-backs. Update
   the parity ledger only with TypeScript source evidence, Go tests, and a
   golden/local acceptance result.
5. Re-run the fixed capacity workload: 32 independent conversations, ten
   turns, 4 KiB payloads. Working threshold is peak RSS <=160 MiB, with zero
   cross-talk and no late results.
6. Only after the first slice is stable, design the shared canonical
   conversation lease/queue for CLI, Telegram, and dashboard.
7. Then define shared semantic and episodic memory scope/checkpoint migration.
8. Deployment and rollback validation come last; do not deploy from this
   handoff.

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
