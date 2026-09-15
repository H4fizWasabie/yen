# ADR 0002: Telegram-first side-by-side pilot

Status: accepted for pilot planning
Date: 2026-09-14

## Decision

Telegram is the primary live pilot channel for the Go runtime. The Go service
may be deployed beside the actively running TypeScript Theoses2 service on
`root@100.101.53.98` (`http://100.101.53.98/`).

The existing Theoses2 service, its files, ports, credentials, and process
lifecycle remain unchanged. Go is intended to become the main runtime only
after parity evidence, rollback validation, and explicit cutover approval.

The pilot uses the existing runtime's LLM credential source without exposing
or copying the secret into the repository. The target model is `glm 5.3 flash`
with high reasoning.

## Telegram constraint

The existing Telegram bot token cannot be used by two long-polling processes
at once. Before deployment, choose one of:

1. a separate pilot Telegram bot token; or
2. an explicitly reviewed webhook/routing arrangement that preserves the
   existing bot's ownership and rollback path.

Pausing or replacing the existing Telegram poller is outside this decision.

## Required pilot evidence

- existing VPS service/unit, ports, storage, and credential source read-back;
- dedicated Go service name, port, storage path, and health check;
- Telegram visible-reply acceptance with the selected routing boundary;
- restart/resume and session/memory read-back;
- rollback to the existing TypeScript service without changing its data;
- no unresolved P0/P1 parity discrepancy before cutover discussion.
