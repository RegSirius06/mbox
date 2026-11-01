# Administrator notes

The canonical command reference is `README.md` plus `mboxctl --help`.

Important state locations:

```text
/etc/mbox/mbox.conf                 human configuration
/etc/mbox/server.json               generated runtime config
/etc/mbox/server.key                secret server X25519 private key
/etc/mbox/server.pub                server public key
/var/lib/mbox/users.db              authoritative users/PSKs/policy
/var/lib/mbox/regru/current/        generated shared-host relay/site bundle
/var/lib/mbox/origin/current/       generated origin proxy snippet
```

Never hand-edit `server.json`; edit `mbox.conf` through `mboxctl config set` (or carefully by hand) followed by `mboxctl config validate && mboxctl sync --restart`.

## Subscription/session controls

A user row remains present when disabled or expired. That allows a paid subscription to be paused/expired and later reactivated without changing its ID or PSK.

Create a two-session, 30-day profile:

```bash
mboxctl user add alice --max-devices 2 --days 30
```

Change the concurrent-session limit:

```bash
mboxctl user devices alice 3
mboxctl user devices alice --unlimited
```

`user devices` and `user limit` are aliases. The current implementation counts concurrent authenticated MBox **sessions**. In a normal Android deployment one running installation has one logical session, so it behaves like a practical device limit. It is not immutable hardware identity: a future one-time enrollment/device credential layer is the correct mechanism for cryptographically strong device counting.

Expiry controls:

```bash
mboxctl user expiry alice --days 30
mboxctl user expiry alice --at 2026-12-31T23:59:59Z
mboxctl user expiry alice --clear
mboxctl user renew alice 30
```

`renew` extends from the existing future expiry when possible, otherwise from the current time; it also re-enables the user and clears revocation state.

Disable/revoke/rotate/expiry changes are applied to the in-memory policy map. Active sessions no longer keep access indefinitely after policy removal: sessions are terminated when their client becomes disabled/revoked/expired, their PSK changes, or a newly lowered session cap requires excess sessions to be closed.

Global `[limits]` remain authoritative even for a user marked `--unlimited`.

## Small-VPS resources

Use:

```bash
mboxctl resources
```

to show configured session/stream/queue limits and live process RSS, peak RSS, threads and open FDs.

The default low-resource profile uses a 64 MiB global queued-byte budget, `GOMEMLIMIT=192MiB` and systemd `MemoryHigh=256M`. `MemoryHigh` is pressure/GC guidance, not an immediate 256 MiB kill limit. See `docs/RESOURCES.md` before increasing limits on a 1 GiB VPS.

## sing-box handoff

The default:

```ini
[server]
upstream = 127.0.0.1:2080
```

means that MBox terminates its transport and sends each TCP stream into a local proxy. The recommended endpoint is a sing-box `mixed` inbound on `127.0.0.1:2080`. sing-box then decides whether the request exits directly or through another outbound server.

See `docs/SINGBOX.md` and `examples/sing-box-router.json`.

## REG.RU shared-hosting site

The decoy `index.html`/`404.html` live on the public shared-hosting web root together with the FastCGI relay. They do **not** belong on the RU/MBox origin VPS.

`mboxctl relay bundle` includes the site under `site/`, but normal `relay deploy` only updates relay files so it cannot unexpectedly overwrite an existing website. To intentionally publish the bundled decoy pages:

```bash
mboxctl relay deploy \
  --ssh USER@HOST \
  --remote-dir /path/to/webroot \
  --site
```

Existing `index.html`/`404.html` get timestamped rollback copies before replacement.

The relay/shared-host frontend and the actual Internet exit are independent. The MBox origin can hand traffic to sing-box and exit from the same VPS or chain it to Finland, Poland or another server.

## Cover traffic

The feature is disabled by default. Existing configurations are upgraded by `install.sh`; `[cover]` defaults are materialized automatically.

Recommended activation sequence:

```bash
mboxctl cover simulate --hours 24
mboxctl cover test
mboxctl cover enable
mboxctl cover status
```

Disable without editing JSON:

```bash
mboxctl cover disable
```

Useful tuning examples:

```bash
mboxctl config set cover.max_rps 4
mboxctl config set cover.max_requests_per_hour 1200
mboxctl config set cover.idle_after_ms 120000
mboxctl config set cover.quiet_weight 35
mboxctl config set cover.casual_weight 40
mboxctl config set cover.active_weight 20
mboxctl config set cover.burst_weight 5
```

`cover test` makes one real GET through the public frontend to the server cover handler. `cover simulate` performs no network I/O.

## Backups on small disks

Every installer run keeps a rollback archive; manual backups also accumulate. Review before deleting:

```bash
mboxctl backup list
mboxctl backup prune --keep 3 --dry-run
mboxctl backup prune --keep 3
```

Pruning is explicit; MBox does not silently remove recovery archives. `backup create` performs a test restore/integrity validation before declaring the backup valid.
