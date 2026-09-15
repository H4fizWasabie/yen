# Side-by-side installation

`install-side-by-side.sh` builds the two Go adapters and installs only:

- `/opt/yen/releases/<commit>/`
- `/etc/systemd/system/yen-telegram-pilot.service`
- `/etc/systemd/system/yen-dashboard-pilot.service`
- `/var/lib/yen/` (or `YEN_DATA_DIR`)

It does not stop, modify, or replace any `theoses2-*` unit. It defaults to
`YEN_START=0`, so installation and service restart are separate actions.

Required root environment files:

- `YEN_CHANNEL_ENV_FILE` — Telegram chat/token, canonical conversation ID, and
  optional dashboard/compaction settings, including
  `YEN_AUTO_COMPACT_TURNS`, `YEN_AUTO_COMPACT_MAX_HISTORY_TURNS`,
  `YEN_AUTO_COMPACT_KEEP_RECENT_TOKENS`,
  `YEN_AUTO_COMPACT_CONTEXT_WINDOW`, `YEN_AUTO_COMPACT_RESERVE_TOKENS`,
  `YEN_AUTO_COMPACT_OVERFLOW`, and optional
  `YEN_AUTO_CONSOLIDATE`. Defaults to `/etc/yen/yen.env`.
- `YEN_PROVIDER_ENV_FILE` — Yen-only provider credentials and optional
  `YEN_MODEL`/`YEN_OPENAI_BASE_URL`. Defaults to `/etc/yen/yen-provider.env`.

Example on a prepared host:

```sh
YEN_SOURCE_DIR=/opt/yen-src \
YEN_PROVIDER_ENV_FILE=/etc/yen/yen-provider.env \
YEN_START=0 \
/opt/yen-src/deploy/install-side-by-side.sh
```

The files use Yen-owned names, for example `YEN_TELEGRAM_BOT_TOKEN`,
`YEN_CANONICAL_CONVERSATION_ID`, and `YEN_OPENROUTER_API_KEY`.

The script performs no credential generation or migration. Back up the Go data
directory before replacing a release, then use the documented health and
rollback checks in [../docs/M9-OPERATIONS.md](../docs/M9-OPERATIONS.md).

For isolated installer tests, `YEN_ROOT` prefixes `/opt`, `/etc`, and `/var`
targets, while `YEN_SYSTEMCTL` can point to a harmless test command. These
seams are not needed on a real host and must not be used to bypass production
service review.
