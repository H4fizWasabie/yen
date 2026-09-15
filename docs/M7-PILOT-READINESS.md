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
- The Go dashboard pilot on `127.0.0.1:30146` resolved a new dashboard tab to
  the same canonical conversation and completed a live request. The shared
  episodic database contained four records after the acceptance request.
- Live memory acceptance completed through the dashboard path: the existing
  favorite-color fact and the pilot marker were retrieved as conversation-
  scoped semantic nodes; the episodic store contained eight records afterward,
  with `cli`, `telegram`, and `dashboard` channel records sharing the same
  conversation ID.
- The Telegram unit was rolled back to `1901f93` and restored to `2a64583`
  from a verified `/var/backups/yen-theoses-go-20260914T145936Z.tgz` archive;
  both Go units stayed active and the dashboard health check remained green.
- Live dashboard acceptance canceled a blocked turn with `/stop`, recorded a
  durable `canceled` queue event, and then settled a second queued turn with
  the response `queued-two`. The Telegram adapter has the same cancellation
  contract in its focused test.

## Gate status

M7 is accepted for the selected side-by-side pilot path: Telegram
authentication/reply, cross-adapter routing, dashboard routing, restart
persistence, live semantic-memory recall, release rollback, cancellation, and
FIFO queued-turn settlement are evidenced. Full Telegram UI/rendering parity
remains outside this pilot. Keep the existing TypeScript bot untouched.
