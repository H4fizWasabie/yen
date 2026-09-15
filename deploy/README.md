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
  optional dashboard/compaction settings. Defaults to
  `/etc/theoses-go/telegram.env`.
- `YEN_PROVIDER_ENV_FILE` — provider credentials and optional model/base URL.
  Defaults to the existing provider environment file used by the pilot.

Example on a prepared host:

```sh
YEN_SOURCE_DIR=/opt/yen-src \
YEN_PROVIDER_ENV_FILE=/etc/theoses-go/provider.env \
YEN_START=0 \
/opt/yen-src/deploy/install-side-by-side.sh
```

The script performs no credential generation or migration. Back up the Go data
directory before replacing a release, then use the documented health and
rollback checks in [../docs/M9-OPERATIONS.md](../docs/M9-OPERATIONS.md).
