# Architecture

## Components

- `server/`: MBox session/authentication/MUX HTTP server.
- `client/`: shared Go client core used on Linux and Android.
- `relay/`: FastCGI reverse relay for a buffering shared-host intermediary.
- `android/`: Android VpnService + HEV wrapper. It builds `../client` for ARM64.
- `tools/mboxctl/`: administrative control plane.
- SQLite: administrative user source of truth only; not in the per-packet data plane.

## Control plane

`mboxctl` owns `/etc/mbox/mbox.conf` and `/var/lib/mbox/users.db`.

On user changes it commits SQLite first, then atomically renders `/etc/mbox/server.json` with enabled client IDs and inline 32-byte PSKs. It sends `SIGHUP` to the server. The server validates the new client map and atomically swaps the in-memory map. Existing sessions keep their already-derived session keys.

General transport/config changes use a service restart because listen/base-path/camouflage routing is initialized at process startup.

## Data plane

A logical MBox session carries multiplexed TCP streams. The intermediary is allowed to buffer each HTTP transaction completely. Therefore the transport does not rely on streaming an HTTP response forever.

UP data is sent in POST requests. DOWN data can be returned in the POST response (piggyback) or retrieved by a short standalone poll.

Exact encrypted UP retries are important: retrying does not allocate a fresh crypto counter for the same logical MUX bytes. The server keeps a bounded response cache keyed by committed counter and replays the cached response if the exact retry arrives.

## Android

Android creates a VpnService TUN and feeds it to HEV, which forwards TCP through the local MBox SOCKS endpoint. Before MBox starts, Java resolves the public endpoint on Android's current physical `Network` and passes the numeric address through `MBOX_CONNECT_IP`; the Go HTTP transport dials that address while preserving the original URL host/TLS SNI.


## Server cover traffic

The optional cover engine runs inside `mbox-server`. It is not a second MBox client and does not require a second server process. Its target is the configured public frontend, so a cover request traverses the same shared-host frontend and returns to the RU origin through the normal `/api/v1/*` relay path.

The server tracks recent authenticated MBox session activity. A successful real OPEN cancels any in-flight cover context and suppresses new cover requests. When no real session is recent, the engine waits a randomized grace and selects one of four weighted profiles (`quiet`, `casual`, `active`, `burst`). Rate and hourly budgets are enforced on attempted HTTP requests, including failures, so a broken frontend cannot create an uncontrolled retry storm.

`/run/mbox-cover-status.json` exposes non-secret runtime state for `mboxctl cover status`.
