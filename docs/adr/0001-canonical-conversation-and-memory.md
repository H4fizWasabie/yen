# ADR 0001: Canonical conversations and memory ownership

Status: accepted
Date: 2026-09-14

## Decision

Theoses2 gives every conversation a durable canonical ID. CLI, Telegram,
dashboard, and future adapters link their adapter identities to that ID; they do
not derive or merge conversations from cwd, chat ID, or user ID.

Each conversation has one active turn and a FIFO queue. Turns have durable IDs;
cancellation targets the active turn and does not discard queued turns.

The memory engine is shared by all adapters and has engine, owner, workspace,
and conversation namespaces. Episodic memory belongs to the canonical
conversation. Semantic memory is owner- or workspace-scoped by default.

Existing TypeScript data is migrated only by an explicit, additive, idempotent
operation. Source data is never rewritten or deleted by migration.

## Rationale

This keeps Mino's simple per-session ownership model while making cross-channel
sharing explicit and auditable. It prevents accidental merges and preserves a
rollback path for the existing channel-keyed data.
