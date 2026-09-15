# Theoses2 Go rewrite

This file is the repository index and routing contract. Read it first, then
load only the files named for the current task. Detailed behavior belongs in
the linked milestone documents, not here.

## Where we are

This is a behavior-compatible Go rewrite of Theoses2. The current slice is a
side-by-side Go Telegram and dashboard pilot backed by one canonical
conversation, shared semantic/episodic memory, an agent loop, one
OpenAI-compatible SSE provider, and the `read` tool. It is not parity-signed
off and has not replaced TypeScript.

The TypeScript oracle is pinned to commit
`3a910426a0db91570392c20c281ae8dfd82e01a1`. Use [docs/UPSTREAM.md](docs/UPSTREAM.md)
for the source boundary. The full Theo Bible stays upstream.

## Route by task

| Task | Load | Completion criterion |
|---|---|---|
| Resume the next implementation task | [handoff.md](handoff.md), [docs/M7-PILOT-READINESS.md](docs/M7-PILOT-READINESS.md), [docs/M10-PARITY-REPORT.md](docs/M10-PARITY-REPORT.md) | The selected gap has a failing Go test or live acceptance, a minimal fix, and passing verification. |
| Check scope, compatibility, or deferred features | [docs/M0-INVESTIGATION.md](docs/M0-INVESTIGATION.md), relevant sections of [docs/M1-CONTRACTS.md](docs/M1-CONTRACTS.md) | The change is recorded in the parity ledger or explicitly deferred. |
| Compare behavior with TypeScript | [docs/UPSTREAM.md](docs/UPSTREAM.md), [docs/M1-GOLDEN-TRACE.md](docs/M1-GOLDEN-TRACE.md), the cited baseline source path | The claim has TypeScript evidence, a Go test, and a golden or acceptance result. |
| Work on memory, concurrency, or deployment | [docs/M1-BASELINE.md](docs/M1-BASELINE.md), sections 7–9 of [docs/M1-CONTRACTS.md](docs/M1-CONTRACTS.md) | The workload, threshold, read-back, and rollback status are recorded. |
| Work on shared CLI/Telegram/dashboard sessions | [CONTEXT.md](CONTEXT.md), [docs/adr/0001-canonical-conversation-and-memory.md](docs/adr/0001-canonical-conversation-and-memory.md), [docs/M7-PILOT-READINESS.md](docs/M7-PILOT-READINESS.md) | Canonical identity, lease/queue ownership, migration, and ordering remain evidenced. |
| Work on semantic or episodic memory | Section 8 of [docs/M1-CONTRACTS.md](docs/M1-CONTRACTS.md) | Scope, checkpoint, migration, and restart behavior are decided before code. |
| Record or resume a session | [handoff.md](handoff.md) | The next agent can start without reconstructing prior decisions. |

## Working process

1. Classify the request using the route table.
2. Read the selected contract and inspect the cited source at the pinned
   TypeScript commit when behavior matters.
3. Choose one vertical slice. Write one seam-level failing Go test first.
4. Implement the smallest change that makes that test pass.
5. Run `go test ./...`, `go test -race ./...`, and `go vet ./...`.
6. For compatibility work, update the parity ledger only after evidence
   exists. Update [handoff.md](handoff.md) when the pickup point changes.

Done means the requested slice is tested, its evidence is recorded, and the
next unresolved contract is visible. Do not broaden the slice to make a demo
look complete.

## Canonical sources

- Behavioral truth: pinned TypeScript source, not comments or copied docs.
- Rewrite scope and decisions: `docs/M0-INVESTIGATION.md` and
  `docs/M1-CONTRACTS.md`.
- Current implementation evidence: `docs/M2-FIRST-SLICE.md`.
- Current pickup state: `handoff.md`.
- Go behavior: tests beside the package they exercise.
- Build/toolchain truth: `go.mod` and the commands above.

## Hard boundaries

- Keep the TypeScript runtime operational until final parity and rollback
  signoff.
- Keep changes narrow and test-first; preserve unrelated work.
- Keep CLI, Telegram, and dashboard on the canonical conversation ID when
  shared mode is enabled; preserve adapter-local behavior only for legacy
  deployments without that setting.
- Keep the TypeScript runtime operational until the parity report's remaining
  live gates and rollback window are closed.
- Do not modify the original repository from this workspace.
