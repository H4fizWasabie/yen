# M1 TypeScript golden trace

Source: commit `3a910426a0db91570392c20c281ae8dfd82e01a1`.

The trace was produced in an isolated baseline archive with the faux provider
and records event types only; streaming update counts are not fixed. The event
implementation is `packages/agent/src/agent-loop.ts:155-371`, with tool
persistence in `packages/coding-agent/src/core/agent-session.ts:662-756`.

```text
success
agent_start, turn_start, message_start(user), message_end(user),
message_start(assistant), message_update..., message_end(assistant),
turn_end, agent_end

tool
agent_start, turn_start, message_start(user), message_end(user),
message_start(assistant), message_update..., message_end(assistant toolUse),
tool_execution_start, tool_execution_end, message_start(toolResult),
message_end(toolResult), turn_end, turn_start, message_start(assistant),
message_update..., message_end(assistant), turn_end, agent_end

error
agent_start, turn_start, message_start(user), message_end(user),
message_start(assistant), message_end(assistant error), turn_end, agent_end

abort
agent_start, turn_start, message_start(user), message_end(user),
message_start(assistant), message_update..., message_end(assistant aborted),
turn_end, agent_end
```

Observed terminal outcomes were `stop`, `stop`, `error`, and `aborted`.
The tool trace used tool ID `calc-1`; the first turn ended with
`tool_execution_start`/`tool_execution_end` before the continuation turn.

This is TypeScript evidence only. A Go parity claim requires the same
normalized trace from a Go test and matching persisted session read-back.

The Go loop now has a normalized-trace test covering success, tool, provider
error, and abort outcomes. Its tool boundary emits
`message_end:assistant:toolUse` and preserves the pinned event ordering;
the session implementation now uses TypeScript-compatible short random entry
IDs and has a persisted tool-turn read-back fixture.
