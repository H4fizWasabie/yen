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

## 2026-09-16 isolated VPS experiment (not fresh-host evidence)

With explicit authorization, a disposable release was installed on the pilot
VPS under `/opt/yen-goal7-experiment-20260916/root`, with separate data and
secret paths, dashboard port `30147`, and units
`yen-goal7-telegram-20260916.service` and
`yen-goal7-dashboard-20260916.service`. The existing `/opt/yen`, `/etc/yen`,
`/var/lib/yen`, and `yen-*-pilot.service` units were not modified. The
experimental Telegram wrapper used a localhost stub and did not poll the real
bot API.

`YEN_START=0` produced no experimental processes before the units were
started. Both experimental units then reached `active`, and
`http://127.0.0.1:30147/healthz` returned `{"ok":true}`. The isolated data
archive was `/var/backups/yen-goal7-vps-isolated-20260916.tgz` (567 bytes,
mode `600`, SHA-256
`cf989dbed5a6e054fa7da8d98482224d5484cbf6b397b9887d5d5ef8cdb0fecf`); its
listing validated, extraction was byte-for-byte verified for
`memory/episodes.db`, and the release switch/return rollback both passed
health checks. After cleanup, both original pilot units remained active and
`30146/healthz` still returned `{"ok":true}`.

This was an isolated existing-host experiment, not a genuinely fresh host;
the Goal 7 fresh-host gate therefore remains open. An archived source transfer
also required explicit `YEN_RELEASE_ID=103487d` because `git archive` omits
the `.git` directory; a normal checkout does not require that override.
