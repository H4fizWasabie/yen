# Side-by-side installation

`install-side-by-side.sh` builds the two Go adapters and installs only:

- `/opt/yen/releases/<commit>/`
- `/etc/systemd/system/yen-telegram-pilot.service`
- `/etc/systemd/system/yen-dashboard-pilot.service`
- `/var/lib/theoses-go-telegram/` (or `YEN_DATA_DIR`)

It does not stop, modify, or replace any `theoses2-*` unit. It defaults to
`YEN_START=0`, so installation and service restart are separate actions.

Required root environment files:

- `YEN_CHANNEL_ENV_FILE` — Telegram chat/token, canonical conversation ID, and
  optional dashboard/compaction settings, including
  `THEOSES_AUTO_COMPACT_TURNS`, `THEOSES_AUTO_COMPACT_OVERFLOW`, and optional
  `THEOSES_AUTO_CONSOLIDATE`. Defaults to
  `/etc/theoses-go/telegram.env`.
- `YEN_PROVIDER_ENV_FILE` — Yen-only provider credentials and optional model/base
  URL. Defaults to `/etc/theoses-go/yen-provider.env`; it must not point at the
  existing Theoses provider environment.

Example on a prepared host:

```sh
YEN_SOURCE_DIR=/opt/yen-src \
YEN_PROVIDER_ENV_FILE=/etc/theoses-go/yen-provider.env \
YEN_START=0 \
/opt/yen-src/deploy/install-side-by-side.sh
```

The script performs no credential generation or migration. Back up the Go data
directory before replacing a release, then use the documented health and
rollback checks in [../docs/M9-OPERATIONS.md](../docs/M9-OPERATIONS.md).

For isolated installer tests, `YEN_ROOT` prefixes `/opt`, `/etc`, and `/var`
targets, while `YEN_SYSTEMCTL` can point to a harmless test command. These
seams are not needed on a real host and must not be used to bypass production
service review.
