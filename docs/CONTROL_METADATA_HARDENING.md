# MBox control metadata authentication — beta.3 strict control MAC

**Status 2026-11-05:** origin was configured with `auth.require_authenticated_control=true` and the updated Android client continued to work via REG.RU. The public beta.2.1 Android APK omits `cmac` and cannot be used with this strict mode. Noise records and PSK profiles are unchanged. Instructions below describe the historical staged rollout and do not imply beta.3 is backward compatible with the publicly released beta.2.1.


## Threat model and protocol

The shared-hosting relay can read, drop, reorder, duplicate, and edit HTTP request
paths and query parameters. Without independent client-to-origin authentication,
it could fabricate `ack` and release an undelivered DOWN replay batch, or send
an unauthenticated `POST /session/{sid}` to close an active connection.

The client now emits `cmac` (43-character unpadded base64url HMAC-SHA256) on:

- DOWN GET: signs `purpose=DOWN`, method `GET`, `seq`, `ack`, `max`.
- UP POST: signs `purpose=UP`, method `POST`, `seq`, `ack`, empty `max`.
- CLOSE POST: signs `purpose=CLOSE`, method `POST`, counters 0,0 and empty `max`.

The signing key is session-scoped:

`HMAC-SHA256(PSK, "MBOX-HTTP-CONTROL-V1\0KEY\0" || noise_channel_binding || session_id)`

Each request MAC contains a domain-separated, length-prefixed encoding of
method, logical purpose and `max`, followed by binary big-endian `seq` and
`ack`. The Noise PSK is never transmitted to the relay. Equality checks use
constant-time comparison. The same session key cannot authenticate a different
Noise session or a different logical endpoint.

**Do not sign the public HTTP pathname**. REG.RU FastCGI strips `/relay.fcgi`
before forwarding to origin, so path-based MACs would break the existing
cascade. The server determines the logical purpose from the matched endpoint
handler; this cannot be selected by the relay independently of the handler.

## Compatibility and safe rollout

1. **Archive the current server binary, config and rollback materials**.
   Do not delete the current working release.
2. Apply the patch to the Arch source tree and build/test the new Android
   client. The current server ignores the extra `cmac` query parameter, so
   this first stage is backward-compatible (but **not yet enforcing security**).
3. Upgrade and smoke-test the server on a separate staging instance using
   `auth.require_authenticated_control=false` (the default). The server
   validates a supplied MAC, but accepts missing MACs for old clients. A
   compromised relay can still strip MACs **in this transitional mode**.
4. Once every client is on the patched build, perform a controlled production
   upgrade with rollback ready. Check successful full-path connectivity through
   REG.RU and record/replay behavior. Retire old clients.
5. Only **after** verifying every client emits MACs, activate strict mode:

   `mboxctl config set auth.require_authenticated_control true`

   This regenerates runtime config and **restarts the server**, dropping
   existing sessions. Schedule it; do **not** execute prematurely. Confirm
   `auth.require_authenticated_control` is `true` in the generated server
   runtime config. An older Android/client build that omits `cmac` will not
   be able to transfer data or close sessions in strict mode.
6. Rollback if needed by restoring the previous binaries/config and the
   previous compatible client. To disable strict mode on the new server:

   `mboxctl config set auth.require_authenticated_control false`

   The rollback command also restarts the service. Transition mode does not
   meet the threat model of an untrusted relay.

**This historical patch originally did not deploy itself; field deployment was subsequently confirmed on 2026-11-05.** Do not activate strict mode on
an unrehearsed system or before all clients are updated.

## Guarantees and exclusions

- A compromised relay cannot create a *new* valid ACK, sequence value, max,
  or CLOSE command in strict mode without the session's PSK-derived key.
- Captured valid signed requests can be replayed, but existing idempotent
  DOWN response caching and monotonic ACK logic remain responsible for
  replay safety. Server-side tests exercise forged ACKs and exact replay.
- The relay can still deny service, drop packets, delay delivery, observe
  metadata and selectively replay **previously valid** client operations.
- An exposed user PSK, compromised Android OS, compromised origin, or
  incomplete fail-closed rules are outside this mechanism's guarantees.
- `cmac` does **not encrypt** `seq`/`ack`; hiding those values would require a
  separate encrypted HTTP control envelope with a coordinated wire change.
- Extra URL parameters can affect statistical fingerprinting; assess them in
  the fingerprint laboratory before production release.

## Incremental user management

`clientPolicies` now has an account -> identity index. Reload/delete of a
credential updates its own aliases under `clientAuthMu` rather than cloning
all users and their PSKs. All mutations and post-commit session revocations
are serialized by `credentialUpdateMu`. The global map is rebuilt only when
loading/reloading the *entire* runtime config. Account-ID moves (even with
unchanged PSK) revoke the old sessions to prevent quota/identity confusion.

This makes credential-map work independent of unrelated users, but active
session revocation still scans the in-memory session table. Full 10/50/100/
1000 client load and multi-day soak testing remain outstanding.

## Local verification

```sh
cd ~/src/mbox-main
GO111MODULE=off go test ./common/controlmeta ./server ./client ./relay
GO111MODULE=off go test -race ./common/controlmeta ./server ./client ./relay
python3 -m pytest -q tests
python3 tools/check-source-layout.py
cd android && ./tools/build-debug.sh
```

The Android build must be checked on Arch/CI with the Android SDK; local
Go/Python tests do not prove real-world network fail-closed safety.
