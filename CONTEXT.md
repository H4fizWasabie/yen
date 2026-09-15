# Theoses2 domain glossary

## Conversation

A durable user-facing thread with one canonical identity. It is independent
of the channel used to reach it.

## Adapter identity

The identity supplied by a channel, such as a CLI workspace, Telegram chat, or
dashboard session. It is a link to a conversation, not the conversation itself.

## Turn

One submitted user prompt and its agent outcome. A turn has its own identity so
that cancellation targets one active operation without removing queued turns.

## Workspace

The project or operating context associated with a conversation. Workspace
facts are distinct from owner-wide facts.

## Memory namespaces

Theoses2 memory is divided into engine, owner, workspace, and conversation
namespaces. Channels do not own memory namespaces.
