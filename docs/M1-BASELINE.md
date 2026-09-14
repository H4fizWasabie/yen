# M1 TypeScript baseline evidence

Status: partial baseline capture; no Go implementation or deployment had
started when these measurements were recorded.

## Reproduction boundary

The baseline was exported from commit
`3a910426a0db91570392c20c281ae8dfd82e01a1` into a temporary directory. The
original checkout was not changed. Dependencies were installed there with
`npm ci --ignore-scripts --no-audit --no-fund`.

Environment:

- Node `v22.23.0`
- npm `10.9.8`
- Go `1.22.2` (not exercised by this baseline)
- Linux x86_64

## Focused test evidence

| Scope | Command | Result |
|---|---|---|
| Agent loop | `npm --workspace=theoses-agent-core test -- test/agent-loop.test.ts` | 1 file, 24 tests passed |
| Agent lifecycle/e2e | `npm --workspace=theoses-agent-core test -- test/agent.test.ts test/e2e.test.ts` | 2 files, 32 tests passed |
| Session/print/episodic | `npm --workspace=theoses-coding-agent test -- test/session-manager/build-context.test.ts test/session-manager/custom-session-id.test.ts test/session-manager/save-entry.test.ts test/session-manager/sdk-session-manager.test.ts test/print-mode.test.ts test/episodic-store.test.ts` | 5 files, 37 tests passed |
| Read/path safety | `npm --workspace=theoses-coding-agent test -- test/block-images.test.ts test/audit-file-safety.test.ts` | 2 files, 12 tests passed |
| Agent-session prompt/queue | `npm --workspace=theoses-coding-agent test -- test/suite/agent-session-prompt.test.ts test/suite/agent-session-queue.test.ts` | 2 files, 33 tests passed |

The test-runner maximum RSS observations were approximately 223 MiB for the
agent-loop command and 245 MiB for the coding-agent command. These include
Node, Vitest, transforms, and test imports; they are not an application idle
RSS or a memory ceiling.

The exact commit does not track generated model-data JSON, so the temporary
baseline needed those ignored build artifacts for module loading. They were
copied from the clean sibling checkout only as generated data; no source file
from its newer commit was imported. This is a harness caveat, not an upstream
source change.

## Deterministic capacity probe

The temporary baseline ran `measure-capacity.mjs` with one, eight, and 32
independent agents; each ran ten turns with a 4 KiB user/assistant payload and
a faux provider. Every run settled all agents and retained 20 messages per
agent:

| Agents | Elapsed | Peak RSS |
|---:|---:|---:|
| 1 | 10.7 s | 149 MiB |
| 8 | 10.8 s | 159 MiB |
| 32 | 11.0 s | 159 MiB |

These are process-level observations with a deterministic faux provider.
Startup, garbage collection, and provider token pacing dominate this small
run. They establish a repeatable comparison workload, not a production
ceiling.

## Baseline blocker resolved

The previously blocked agent and agent-session suites now pass after the
ignored generated assets were supplied in the temporary archive. No upstream
source was changed.

## Capacity and deployment status

The remaining required runs are:

1. deterministic provider workload with one and N independent conversations;
2. same-conversation ordering, rejection/queue, abort, and late-result checks;
3. session-file and memory-store read-back after restart;
4. clean build/start/health/restart/rollback sequence in an isolated staging
   environment.

The pinned TypeScript tree also completed `npm run build:offline`, and the
built CLI returned exit code 0 for `node packages/coding-agent/dist/cli.js
--help`. This proves local build/startup wiring only; it is not a deployment
or rollback result.

No live provider credentials, deployment target, cutover, or VPS action was
used or authorized. The deterministic concurrency workload is characterized;
the memory ceiling and deployment SLO remain unknown.

## M1 consequence

M1 contract extraction has produced the implementation gate and the first Go
slice is now in progress. Local rollback, live-provider acceptance, and target
threshold comparison remain release gates; none has been claimed here.
