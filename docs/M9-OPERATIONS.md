# M9 operations

Date: 2026-09-14

## Side-by-side pilot layout

- Telegram unit: `yen-telegram-pilot.service`
- Dashboard unit: `yen-dashboard-pilot.service`
- Existing TypeScript units: `theoses2-telegram.service`,
  `theoses2-telegram-staging.service`, `theoses2-dashboard.service`, and
  `theoses2-dashboard-staging.service`
- Go data directory: `/var/lib/yen`
- Go dashboard health: `127.0.0.1:30146/healthz`
- Go release layout: `/opt/yen/releases/<commit>/`
- Go secret file: `/etc/yen/yen.env`, mode `600`; never commit or
  print its values.

The TypeScript units and their ports, working directories, data, and pollers
are not modified by Go rollout or rollback.

## Read-back

```sh
systemctl is-active yen-telegram-pilot.service yen-dashboard-pilot.service
curl -fsS http://127.0.0.1:30146/healthz
journalctl -u yen-telegram-pilot.service -u yen-dashboard-pilot.service --since "10 minutes ago" --no-pager
du -sh /var/lib/yen
```

## Backup and rollback

Back up the Go data directory before changing a release. Exclude the systemd
secret file; it is managed separately and must not enter an archive.

```sh
stamp=$(date -u +%Y%m%dT%H%M%SZ)
tar -C /var/lib -czf "/var/backups/yen-$stamp.tgz" yen
tar -tzf "/var/backups/yen-$stamp.tgz" >/dev/null
```

Rollback is a unit-only change: point each Go unit at the previous release,
run `systemctl daemon-reload`, restart the Go units, and repeat the read-back.
Do not stop or edit any `theoses2-*` unit. Restore the data archive only when
the failure is data-related; release rollback alone preserves current data.

## Current gate

The pilot has passed live health, systemd restart, OpenRouter smoke, dashboard
routing, verified backup creation, rollback/restore, cancellation, and FIFO
queue acceptance. `deploy/install-side-by-side.sh` now provides a reproducible
fresh-host installation path. It passed an isolated user-namespace acceptance
with a temporary root, fake systemctl, both binaries and wrappers present,
unit files present, channel env mode `600`, and `YEN_START=0` proving no start.
The documented unit/data/secret layout remains the operational contract.

## Latest live backup

On 2026-09-15 the current side-by-side pilot data was archived and verified at
`/var/backups/yen-20260915T093942Z.tgz` (11,333 bytes, mode `600`). Archive
listing validation succeeded; all Yen and Theoses2 units remained active and
Yen `/healthz` returned `{"ok":true}`.
