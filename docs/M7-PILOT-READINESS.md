# M7 pilot readiness

Date: 2026-09-14

## Evidence

- Cross-built Linux amd64 Go binaries from commit `7b2334a`.
- A one-shot Go CLI request reached the existing OpenRouter credential source
  and returned the requested model response from `z-ai/glm-5.3-flash`.
- The Go Telegram pilot was installed under its own `yen-telegram-pilot.service`
  and `/var/lib/theoses-go-telegram`; no existing Theoses2 unit, file, port, or
  poller was changed.
- Telegram rejected the supplied pilot token with HTTP `401 Unauthorized` in
  both the Go poller and a direct `getMe` request.
- The dedicated pilot unit was stopped and disabled, and its token file was
  removed. Existing TypeScript units remained active.

## Gate status

M7 is blocked on a valid replacement Telegram token. No live Telegram reply,
restart/resume read-back, shared-memory acceptance, or rollback acceptance is
claimed. Retry with a freshly issued token; keep the existing TypeScript bot
untouched.
