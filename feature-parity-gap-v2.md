# Yen / Theoses2 feature-parity gap audit — v2

Date: 2026-09-16  
Baseline: Yen branch `feat/feature-parity-gap-v1` at `bb9e9e7`  
Oracle: Theoses2 TypeScript pinned by `docs/UPSTREAM.md` at
`3a910426a0db91570392c20c281ae8dfd82e01a1`

This is an audit only. It records gaps still visible after the v1 parity work;
it does not claim that any item is implemented or parity-signed off.

Explicitly excluded from this audit: semantic/episodic memory, channel
adapters, and channel/session identity or migration behavior.

## Findings

### P1 — Tool definitions and tool-result contracts are narrower than the oracle

Yen's agent seam exposes `Tool` as only `Name()` plus
`Execute(ctx, args) (string, error)` (`internal/agent/loop.go:91-94`). The
provider seam passes only `[]string` tool names
(`internal/agent/loop.go:61-63`); providers then reconstruct a small
hard-coded schema in `internal/provider/openai.go:723+` and equivalent provider
files. External tools do retain `description` and `schema` in
`internal/codingagent/external.go:17-24`, but those fields are not exposed
through `agent.Tool` and therefore are not sent to the model.

Theoses2's `AgentTool` carries a typed `parameters` schema, a UI `label`, an
optional argument-preparation hook, execution mode, and an execution result
with content, structured details, usage, dynamically added tool names, and a
`terminate` hint (`packages/agent/src/types.ts:361-408`). The loop also uses
those fields when validating and batching calls (`packages/agent/src/agent-loop.ts:420-430,602-762`).

Impact: built-in tools currently work through Yen's hard-coded schemas, but
extension and external tools cannot achieve oracle-level schema validation,
descriptions, dynamic tool introduction, incremental updates, structured
details, or per-result termination semantics.

### P1 — Provider tool-call metadata is lost in the normalized Go model

Yen's `ToolCall` stores only `ID`, `Name`, and `Args`
(`internal/agent/loop.go:30-34`). Its normalized `Message` likewise has flat
thinking/text fields rather than provider-neutral content blocks
(`internal/agent/loop.go:10-28`). Theoses2's tool-call content preserves
`thoughtSignature` and an optional Responses `namespace`, while thinking
content can preserve a redacted marker
(`packages/ai/src/types.ts:365-389`).

Impact: Google thought continuity, namespaced/dynamically sourced Responses
tools, and redacted-reasoning replay cannot be preserved through Yen's common
transcript model even where individual providers receive the initial event.

### P1 — Compaction is a single transcript summary, not the oracle's full
compaction policy

Yen's `compactConversation` builds one text transcript, calls the summary
provider once, and appends one summary entry
(`internal/runtime/runtime.go:418-490`). It supports the v1 token/turn cut
points, previous summary, and usage accounting, but no evidence was found for
the oracle's richer compaction settings and context construction.

Theoses2 defines compaction settings for enabled state, reserve tokens, recent
tokens, and maximum history turns, plus provider retry settings
(`packages/coding-agent/src/core/settings-manager.ts:18-40,853-875`). Its
compaction implementation also has dedicated handling for split turns,
previous summaries, file-operation context, and turn-prefix summarization
(`packages/coding-agent/src/core/compaction/compaction.ts:1097+`).

Impact: equivalent settings do not necessarily produce equivalent retained
context or summaries, especially after interrupted/partial turns and file
operations.

### P1 — Extension/resource lifecycle parity is incomplete

Yen's loader discovers `.theoses/extensions`, global extensions, and configured
paths, then bridges commands, renderers, and provider/tool hooks
(`internal/extensions/loader.go:54-117, registerBridge`; `internal/extensions/registry.go:21-35,127+`). The current parity report records the remaining boundary as full
extension context/actions, native registered tools/providers,
resource-discovery events, and native TUI components
(`docs/M10-PARITY-REPORT.md:1028`). It also records full resource-loader
precedence, diagnostics, and extension discovery as open
(`docs/M10-PARITY-REPORT.md:1032`).

Theoses2's resource loader exposes extension, skill, prompt, theme, agent-file,
diagnostic, reload, and resource-extension APIs
(`packages/coding-agent/src/core/resource-loader.ts:18-57`), and its extension
discovery supports direct files, one-level `index` entries, package manifests,
explicit paths, trust gating, and reloadable resource state
(`packages/coding-agent/src/core/extensions/loader.ts:680-810`).

Impact: Yen can load and intercept extensions, but cannot yet provide the full
oracle resource lifecycle or extension-facing action/tool/provider surface.

### P2 — OpenAI prompt-cache keys are not clamped to the oracle's limit

Yen sends the raw session identifier as `prompt_cache_key`
(`internal/provider/cache.go:20-35`; `internal/provider/openai.go:201-206`),
with no length clamp. Theoses2 defines a 64-character maximum and clamps the
key before sending it (`packages/ai/src/api/openai-prompt-cache.ts:1-7`).

Impact: long conversation/session identifiers can produce a provider request
that differs from the oracle or is rejected instead of using the intended
prompt cache.

## v1 items verified closed and therefore not repeated

- Agent interception hooks and continuation retry path.
- Per-file mutation serialization for write/edit operations.
- Anthropic cache-control placement and retention settings.
- Explicit-zero and nested retry/compaction settings merge behavior.
- Expanded structural system-prompt guidance.
- Task-boundary detection seam.

## Suggested implementation order

1. Give the agent/provider seam a real tool definition and result type, while
   preserving the minimal built-in adapters.
2. Extend normalized provider metadata for signatures, redaction, and tool
   namespaces.
3. Close compaction behavior with focused golden tests before changing more
   settings.
4. Expand resource/extension lifecycle only where an oracle behavior has a
   concrete Go acceptance test.
5. Add the 64-character cache-key clamp as a small provider test and fix.

No implementation, deployment, release, memory change, or channel/session
change was made as part of this audit.
