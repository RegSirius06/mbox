# Resource profile for small VPSes

[Русская версия](ru/RESOURCES.md)

MBox is intended to remain usable on inexpensive 1 vCPU / 1 GiB RAM VPSes. Idle memory is normally very small; the important case is bounded memory under concurrent traffic.

The generated `[limits]` section controls server-wide resource ceilings:

```ini
[limits]
max_sessions_global = 128
default_max_sessions_per_client = 4
max_streams_per_session = 128
max_total_streams = 1024
max_stream_opens_per_second = 32
max_session_opens_per_minute = 180
max_concurrent_handshakes = 32
max_handshake_attempts_per_second = 64
max_down_queue_bytes = 4194304
max_down_replay_bytes = 8388608
global_queued_bytes = 67108864
```

The pre-auth handshake caps bound CPU work from invalid OPEN floods before a session is created; denied probes still receive the same generic camouflage 404. The default global transport queue budget is 64 MiB. This is not the entire process RSS: Go stacks, HTTP buffers, crypto state and kernel socket buffers also consume memory. The systemd service additionally sets `GOMEMLIMIT=192MiB` and `MemoryHigh=256M` as soft pressure/GC guidance; it does not impose a kill-on-300-MiB policy.

Use:

```bash
sudo mboxctl resources
```

to see the configured limits plus the running process RSS, peak RSS, thread count and open FD count.

## Real-device calibration

On 2026-11-02 a real Android stress run switching aggressively between Telegram, Instagram and YouTube reached a peak of 64 concurrent MBox streams. After the phone was left idle the count fell progressively (for example 44 -> 35 -> 28 -> lower), so a stream-lifecycle leak was **not** observed. The default was therefore raised from 64 to 128 to leave roughly 2x headroom without weakening the global `max_total_streams=1024` ceiling.

## Scaling guidance

For 1 GiB VPSes, start with the defaults and increase limits only after measuring. `--unlimited` on a user means unlimited relative to that user's per-client session cap; global server limits still apply so one shared profile cannot allocate unbounded RAM/FDs.

The server no longer logs every TCP data chunk. Lifecycle, authentication/revocation and errors remain logged; eliminating per-chunk logs substantially reduces journal write volume on small disks.

SQLite contains credentials/metadata only and stays small. The main disk-growth source in a long-running deployment should therefore be the system journal and unrelated services, not MBox data queues (which are memory-only).

## Disk footprint and backup cleanup

A stripped x86-64 development build of the five installed Go binaries (`mbox-server`, `mbox-client`, `relay.fcgi`, `mbox-key`, `mbox-link`) is roughly 21 MiB in total; exact size varies with Go/toolchain. SQLite user metadata and generated JSON/INI files are tiny by comparison.

Installer rollback archives under `/root/mbox-preinstall-*.tar.gz` and administrator backups can accumulate across upgrades. MBox does not silently delete recovery copies. Inspect/prune them explicitly:

```bash
sudo mboxctl backup list
sudo mboxctl backup prune --keep 3 --dry-run
sudo mboxctl backup prune --keep 3
```

`backup create` now immediately performs an isolated test restore, SQLite integrity check and config/runtime parse before reporting success.

The RAM/disk figures reported by `mboxctl resources` cover the MBox server process itself. sing-box, Caddy/nginx and other routing components are separate processes and must be budgeted separately on a 1 GiB VPS.

## Runtime counters

`mbox-server` writes a small non-secret snapshot to `/run/mbox/status.json` every five seconds. `/run` is normally tmpfs, so this does not create journal/disk churn. It contains only aggregate active session/stream/queued-byte counts and active-session counts by client ID; it does not contain PSKs or payload data.

`mboxctl user list` uses this snapshot to show `ACTIVE` next to each configured `LIMIT`. `mboxctl resources` shows the aggregate snapshot together with `/proc` RSS/FD/thread metrics.
