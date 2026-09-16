# Yen feature-parity gaps v1

Read-only audit against the pinned Theoses2 oracle at commit
`3a910426a0db91570392c20c281ae8dfd82e01a1`.

This note intentionally excludes memory and channel/session behavior.

## Core gaps

| Priority | Gap | Evidence and impact |
| --- | --- | --- |
| P1 | Retry does not continue the existing agent context | Theoses2 exposes `agentLoopContinue` for retries. Yen restarts `RunFrom...` with the original history and prompt, so a provider failure after tool execution can repeat the tool call. |
| P1 | Compaction settings cannot represent zero values | Theoses2 supports `maxHistoryTurns: 0` to disable turn compaction. Yen ignores zero values while merging and applying settings. |
| P1 | Nested provider retry settings are missing | Theoses2 supports provider `timeoutMs`, `maxRetries`, and `maxRetryDelayMs`. Yen only exposes enabled/max retries/base delay. |
| P1 | Anthropic prompt-cache controls are missing | Theoses2 marks the system prompt, final user message, and last tool for caching. Yen sends plain Anthropic system/messages/tools payloads. |
| P1 | Same-file mutation serialization is missing | Theoses2 queues `write` and `edit` operations per canonical file path. Yen directly reads and writes files, so parallel tool calls can race. |
| P2 | Task-boundary detection is absent | Theoses2 maintains task descriptors and detects topic shifts asynchronously. Yen has no corresponding detector or boundary entries. |
| P2 | General agent-loop control hooks are incomplete | Theoses2 supports context transformation, dynamic API-key resolution, `prepareNextTurn`, and `shouldStopAfterTurn`. Yen has provider/tool hooks but no equivalent general loop callbacks. |
| P2 | Core system-prompt behavior is thinner | Theoses2 injects planning, tool-efficiency, no-blocking-wait, recheck, and destructive-action guidance. Yen's baseline prompt only contains identity, tool names, concision, and path guidance. |

## Recommended order

1. Preserve agent context across retries.
2. Serialize same-file mutations.
3. Add Anthropic cache controls.

## Implementation evidence

Implemented on the feature-parity branch:

- retries continue from the accumulated agent messages;
- explicit zero compaction values survive settings merge and runtime apply;
- nested provider timeout/retry/delay settings are modeled and applied;
- Anthropic cache-control markers are supported for system, final content, and last tool;
- write/edit mutations are serialized;
- task descriptors and boundary markers are persisted through an opt-in boundary provider;
- loop context, API-key, next-turn, and stop-after-turn hooks are available and chained through extensions;
- the core prompt includes planning, efficiency, non-blocking, recheck, and destructive-action guidance.

Evidence: `go test ./...`, `go test -race ./...`, and `go vet ./...` pass on the branch. Task-boundary detection remains opt-in because enabling it by default would add an extra provider call to every existing runtime turn; production wiring occurs when a summarization provider is configured.
