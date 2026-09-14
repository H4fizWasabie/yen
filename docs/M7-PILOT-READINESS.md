# M7 pilot readiness

Date: 2026-09-14

## Evidence

- Cross-built Linux amd64 Go binaries from commits `7b2334a`, `c206a8e`, and
  `1901f93`.
- A one-shot Go CLI request reached the existing OpenRouter credential source
  and returned the requested model response from `z-ai/glm-5.3-flash`.
- The Go Telegram pilot was installed under its own `yen-telegram-pilot.service`
  and `/var/lib/theoses-go-telegram`; no existing Theoses2 unit, file, port, or
  poller was changed.
- The first supplied token was rejected with HTTP `401 Unauthorized`; the
  replacement token passed direct `getMe` and Go polling authentication.
- The dedicated pilot runs as `yen-telegram-pilot.service` with its own
  binary, data directory, and secret file. Existing TypeScript units remain
  active and untouched.
- A live Go CLI turn used the same canonical conversation ID and appended to
  the Telegram session; the shared session had seven JSONL messages and the
  episodic store contained one recorded turn afterward.
- Commit `1901f93` was restarted in the live pilot with canonical conversation
  memory enabled; the service stayed active and retained the same session
  file.

## Gate status

M7 is partially accepted for Telegram authentication, visible reply,
cross-adapter session routing, and restart persistence. `/stop`, queued turns,
live memory recall, and rollback acceptance remain open. Keep the existing
TypeScript bot untouched.
