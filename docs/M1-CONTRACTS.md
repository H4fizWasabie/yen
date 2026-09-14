# M1 contract extraction

Status: approved for the shared identity, queue, and memory seams; the local
Go seams are implemented and tested, but no deployment or migration cutover is
authorized. Baseline evidence is in
[M1-BASELINE.md](M1-BASELINE.md), with the event trace in
[M1-GOLDEN-TRACE.md](M1-GOLDEN-TRACE.md).

Baseline: TypeScript commit `3a910426a0db91570392c20c281ae8dfd82e01a1`.

M1 converts the selected M0 slice into language-neutral contracts and fixtures.
It also records the later shared-session/shared-memory seam so that the first
Go slice does not hard-code a channel-specific architecture that must be thrown
away.

## 1. Contract boundary

```text
channel adapter -> canonical conversation lease -> session state/log
                                      |
                              agent turn loop
                              /            \
                       provider          tools
                                      |
                         semantic + episodic memory
```

The original first implementation slice stopped after the CLI adapter, session
state, agent loop, one provider, and `read`. The approved local extension now
implements the shared identity, queue, scoped memory, and adapter seams without
claiming the full TypeScript memory/consolidation surface.

## 2. Canonical conversation identity

### Baseline behavior to preserve during the first parity slice

The baseline `SessionHeader` stores optional `channel` and
`channelSessionId`. Session discovery compares the pair as a JSON tuple via
`sessionLookupKey` (`packages/coding-agent/src/core/session-cwd.ts:9-16` and
`packages/coding-agent/src/core/session-manager.ts:709-717`). The baseline
therefore has separate identities for:

| Adapter | Baseline key |
|---|---|
| CLI | `channel=cli`, `channelSessionId=resolved cwd` |
| Telegram | `channel=telegram`, `channelSessionId=chat ID` |
| Dashboard | `channel=dashboard`, `channelSessionId=session ID` |

The Go CLI slice preserves the CLI key and does not pretend that it is already
cross-channel compatible.

### Later shared-session target

Approved decision: use a channel-independent canonical conversation ID. The

The desired change needs a channel-independent canonical conversation ID. The
adapter identity must become metadata on an input/event, not the durable
session identity. At minimum, the reviewed contract must answer:

1. What stable key joins a CLI cwd, Telegram chat, and dashboard conversation?
2. Can one user have multiple canonical conversations in one cwd?
3. Which adapter is allowed to create a conversation, and how do other
   adapters discover it?
4. How are old channel-keyed sessions joined, left separate, or migrated?
5. Which channel owns presentation-only settings such as Telegram formatting?
6. How are cross-channel replies, attachments, and tool progress represented?

Adapter identities link explicitly to the canonical ID. Go must keep `channel`
and `channelSessionId` as compatibility metadata and must not merge files by
cwd alone.

## 3. Session state and persistence contract

For the first slice:

- session files are JSONL;
- the first record is a versioned session header;
- entries form an append-only tree through `id` and `parentId`;
- a new CLI session records the selected cwd and CLI channel key;
- user, assistant, and tool-result messages are durable session entries;
- model/thinking changes and operation completion are durable where the
  TypeScript path writes them;
- context is rebuilt from the selected leaf, including the baseline active
  context limit and message ordering;
- writes must not publish a partial assistant turn as a completed conversation.

Source authority: `packages/coding-agent/src/core/session-manager.ts:48-71`,
`:441-545`, `:1022-1049`, and `:1279-1329`.

The first fixture set must include:

1. new CLI session before the first assistant message;
2. one user/assistant turn;
3. one user/tool-call/tool-result/assistant turn;
4. reopen and continue from the persisted file;
5. malformed/unknown entry handling without data loss;
6. an interrupted turn showing the exact published-file boundary.

## 4. Agent event contract

For a successful tool turn, normalized events must preserve this order:

```text
agent_start
turn_start
message_start(user)
message_end(user)
message_start(assistant partial)
message_update(...)
message_end(assistant toolUse)
tool_execution_start(read)
tool_execution_end(read)
turn_end(assistant toolUse, toolResults=[read])
turn_start
message_start(assistant partial)
message_update(...)
message_end(assistant stop)
turn_end(assistant stop, toolResults=[])
agent_end
agent_settled
```

The exact stream may contain zero or more update events, but terminal events,
tool-call IDs, tool result ordering, and persistence timing are not optional.
The source path is `packages/agent/src/agent-loop.ts:155-275` and
`:281-371`, with product persistence in
`packages/coding-agent/src/core/agent-session.ts:662-756`.

Required negative cases:

- provider error produces an assistant error and no false success;
- provider abort produces an aborted assistant outcome;
- malformed or invalid tool arguments produce a tool error and safe
  continuation behavior;
- a second prompt for a busy canonical conversation is rejected or explicitly
  queued according to the selected adapter contract;
- tool completion after cancellation cannot append a late result.

## 5. Provider contract

The first real provider is one `openai-completions` model. The provider
boundary accepts:

- model identity and provider;
- system prompt and ordered converted messages;
- active tool schemas;
- abort signal;
- session affinity ID where the provider supports it;
- timeout/retry settings;
- streamed text, thinking, tool-call, usage, done, and error events.

TypeScript authority is `packages/ai/src/models.ts:136-148`, `:690-703` and
`packages/ai/src/api/openai-completions.ts:281-330`, `:699-717`.

M1 fixtures must use a local deterministic HTTP stream for request shape and
terminal/error behavior. The faux provider remains useful for pure agent-loop
tests, but it is not a live provider result.

## 6. Tool contract: `read`

The first tool accepts:

```json
{"path":"README.md","offset":1,"limit":100}
```

It resolves paths relative to the session cwd, reads text, applies the
baseline line/byte limits, returns structured text content, and reports
abortion as an error. Images, automatic resizing, and display rendering are
not in the first Go contract.

Authority: `packages/coding-agent/src/core/tools/read.ts:21-25`,
`:209-245`, `:362-363`.

The fixture set must cover a normal file, missing file, relative traversal or
symlink boundary, truncation, Unicode filename recovery where applicable, and
abort before completion.

## 7. Shared session queue contract for the later multi-channel change

The following queue contract is approved for implementation:

The desired architecture requires serialization at the canonical conversation,
not independently inside each adapter:

- two turns for the same canonical conversation execute in submission order;
- CLI, Telegram, and dashboard cannot each start a concurrent turn against the
  same transcript;
- independent canonical conversations may execute concurrently;
- a stop/abort identifies the specific active turn and does not silently drop
  an unrelated queued message;
- reconnecting an adapter reacquires the same conversation lease;
- session persistence and memory checkpoint updates occur under the same
  ordering contract as the turn that produced them.

Current TypeScript queues are adapter/session-specific: Telegram keeps maps by
chat (`packages/telegram/src/index.ts:406-414`), dashboard keeps a queue per
dashboard session (`packages/dashboard/src/index.ts:282-297`), and
`Agent.prompt` rejects a second active prompt
(`packages/agent/src/agent.ts:347-388`). These are baseline evidence; the Go
queue is the canonical owner after an adapter resolves its link.

## 8. Shared semantic and episodic memory contract for the later change

The following memory contract is approved for implementation:

The baseline already stores semantic nodes under a global memories directory
and episodic records in one SQLite database:

- `FileMemoryStore` exposes `remember` and `saveNote`; nodes are semantic
  Markdown records (`packages/coding-agent/src/core/memory-store.ts:26-46`,
  `:200-251`).
- `EpisodicStore` creates `episodes`, records a time range/summary/semantic-node
  references, and supports keyword, point-in-time, and recent queries
  (`packages/coding-agent/src/core/episodic-store.ts:8-17`, `:53-163`).
- consolidation currently claims/checkpoints by `channel:channelSessionId`
  and writes both stores from a channel-session window
  (`packages/coding-agent/src/core/memory-consolidation.ts:49-81`,
  `:535-580`).

The later shared-memory contract must therefore define:

- the canonical conversation scope of a semantic fact;
- whether episodic records are global, per owner, per project, or per
  canonical conversation;
- how a cross-channel transcript is ordered and deduplicated;
- how consolidation checkpoints move when multiple adapters contribute turns;
- how concurrent consolidation is single-flight per canonical conversation;
- how old channel-keyed checkpoints and memory entries migrate;
- how memory reads and writes behave across process restart and deployment
  rollback.

Go memory uses engine, owner, workspace, and conversation namespaces. No
channel-specific memory store may be introduced.

## 9. Capacity and deployment measurements

M1 must establish a TypeScript baseline before Go claims improvement.

### Memory

Record at minimum:

- idle process RSS and startup time;
- RSS after one session, one tool turn, and one consolidation-sized history;
- peak RSS during provider streaming and tool execution;
- RSS with N independent concurrent conversations;
- session-log, semantic-memory, episodic-database, and artifact sizes.

### Concurrency

Use fixed prompts and a deterministic provider fixture first, then a controlled
live-provider run. Record:

- same-conversation ordering and rejection/queue behavior;
- independent-conversation throughput and latency;
- provider/tool in-flight counts;
- abort latency and late-result absence;
- persistence and memory-checkpoint correctness after concurrent turns.

### Deployment

The acceptance sequence is:

1. build from a clean checkout;
2. install/configure without hidden interactive steps;
3. start and prove health/readiness;
4. run the CLI slice and read back session files;
5. restart and resume;
6. roll back to the previous runtime and resume again;
7. confirm TypeScript remains available as the operational fallback.

No VPS deployment is authorized by M1.

## 10. M1 exit gate

Working acceptance thresholds for the first comparison are:

- memory: peak RSS no greater than 160 MiB for the 32-agent, ten-turn, 4 KiB
  deterministic workload;
- concurrency: 32 independent conversations settle with 20 messages each,
  zero cross-talk, and no late results; same-conversation serialization is a
  separate shared-channel contract;
- deployment: clean build in 10 seconds or less, CLI startup/help in 1 second
  or less, restart/resume read-back succeeds, and rollback completes in 60
  seconds or less in local staging.

These are working thresholds grounded in the measured TypeScript harness, not
claims that Go has met them. A materially different staging environment must
record a new baseline before comparison.

M1 is complete when these are reviewed and checked in:

- language-neutral session, event, provider, and `read` contracts;
- serialized session fixtures and deterministic provider event fixtures;
- normalized TypeScript golden trace for success, tool use, error, abort, and
  resume;
- baseline capacity measurements and the selected Go target thresholds;
- an explicit decision for canonical shared conversation identity and queue
  ownership, or a recorded deferral before multi-channel work;
- an explicit shared semantic/episodic memory scope and checkpoint policy, or a
  recorded deferral before memory work.

Deployment, cutover, and TypeScript removal remain out of scope. The first Go
implementation slice is now in progress under the recorded contracts; release
parity still requires the remaining provider, error, rollback, and live
acceptance evidence.
