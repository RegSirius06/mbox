# Architecture

## Components

- `server/`: MBox session/authentication/MUX HTTP server.
- `client/`: shared Go client core used on Linux and Android.
- `relay/`: FastCGI reverse relay for a buffering shared-host intermediary.
- `android/`: Android VpnService + HEV wrapper. It builds `../client` for ARM64.
- `tools/mboxctl/`: administrative control plane.
- SQLite: administrative user/policy source of truth only; not in the per-packet data plane.
- sing-box (external): recommended local routing/exit engine behind the MBox server.

## Deployment topology

The shared-hosting frontend is separate from both the MBox origin and the final exit:

```text
Android / desktop
       |
       | HTTPS
       v
REG.RU/shared-hosting real website + FastCGI relay
       |
       | HTTPS origin forwarding
       v
MBox server (may be RU, FI, PL, etc.)
127.0.0.1:18449
       |
       | local SOCKS/HTTP proxy handoff
       v
sing-box mixed inbound 127.0.0.1:2080
       |
       +--> direct Internet exit
       +--> Hysteria2/VLESS/SS/SOCKS next hop
       `--> arbitrary sing-box routing policy
```

The decoy HTML belongs to the shared-hosting web root. Nothing requires the MBox server to be on the same host/country as the web frontend or as the final exit.

## Control plane

`mboxctl` owns `/etc/mbox/mbox.conf` and `/var/lib/mbox/users.db`.

User rows carry enabled/revoked state, a 32-byte PSK, an optional expiry timestamp and an optional per-client concurrent-session cap. `0` means use the server default, `-1` means unlimited relative to the per-client cap; global resource limits still apply.

On user changes, `mboxctl` commits SQLite first, atomically renders `/etc/mbox/server.json`, then sends `SIGHUP` to the server. The server validates and swaps the in-memory policy map. Policy reload can terminate active sessions whose user was revoked/disabled/expired, whose PSK changed, or which exceed a newly lowered cap.

General transport/listen/base-path changes use a service restart because those values are initialized at process startup.

## Reliable DOWN transaction

Every DOWN-capable GET poll or POST piggyback request carries:

- `seq`: request sequence used to identify retries of the same HTTP transaction;
- `ack`: highest contiguous DOWN crypto counter already accepted/delivered by the client.

All server DOWN producers share one serialization point. A commit is logically:

```text
apply ACK
  -> select complete queued MUX frames (do not forget them yet)
  -> pad
  -> allocate DOWN crypto counter
  -> AES-GCM seal
  -> register exact ciphertext in replay cache under request seq
  -> only now remove selected frames from pending queue
```

If the HTTP response is lost after server commit, the client retries the exact same `seq`; the server returns the byte-identical encrypted record. Replay entries with encrypted data remain until a later request acknowledges their crypto counter.

GET and POST therefore cannot independently pop batches and assign counters in conflicting order.

## Client DOWN delivery

GET and POST responses never deliver MUX frames directly. They submit encrypted records to one bounded delivery queue/goroutine.

That goroutine authenticates/decrypts, checks counters, buffers a bounded out-of-order window, delivers only contiguous records to the stream layer, and advances the ACK only after frame handling succeeds. Exact replays of already-delivered records are discarded.

## UP scheduling and flow control

UP no longer drains an unbounded shared `bytes.Buffer`. Each stream contributes complete frames to bounded queues. A round-robin scheduler builds records up to `camouflage.up_max_bytes` without splitting a MUX frame.

MUX `WINDOW` frames provide byte credits per stream. A sender may transmit DATA only against granted credit. The receiver replenishes credit only after DATA has actually been written to the destination socket. Thus a slow destination stalls its own stream instead of forcing unbounded buffering or synchronously blocking unrelated streams.

Client/server stream writers are separate goroutines with bounded queues and write deadlines.

## Resource bounds

Server safety ceilings are generated from `[limits]`: global sessions, default sessions/client, streams/session, total streams, open rates, pending DOWN bytes, replay bytes and process-wide queued bytes. See `docs/RESOURCES.md`.

A user marked `--unlimited` bypasses only that user's per-client session cap; it does not bypass global safety limits.

## sing-box routing boundary

The default `server.upstream = 127.0.0.1:2080` is intended for a local sing-box `mixed` inbound. MBox itself deliberately does not select the final Internet exit. See `docs/SINGBOX.md`.

## Android

Android creates a VpnService TUN and feeds it to HEV, which forwards TCP through the local MBox SOCKS endpoint. Before MBox starts, Java resolves the public endpoint on Android's current physical `Network` and passes the numeric address through `MBOX_CONNECT_IP`; the Go HTTP transport dials that address while preserving URL host/TLS SNI.

Provisioning imports are staged into a new immutable generation. The app shows the server public-key SHA-256 fingerprint and requires confirmation, writes the generation's key material/config, then atomically replaces active `client.json`. Desired VPN running/stopped state is persisted for `START_STICKY` recovery.

## Server cover traffic

The optional cover engine runs inside `mbox-server`. Its target is the configured public frontend, so a cover request traverses the same shared-host frontend and returns to the MBox origin.

A successful real OPEN suppresses new cover traffic. When no real session is recent, the engine uses randomized profile timing subject to rate/hourly budgets. Cover affects the frontend traffic history; it does not make last-mile client traffic intrinsically indistinguishable from a browser.

Runtime state is written under `/run/mbox/cover-status.json` for `mboxctl cover status`.
