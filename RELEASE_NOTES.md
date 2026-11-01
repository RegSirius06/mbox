# MBox 1.0.0-beta.2

beta.2 combines the reliability/resource-control hardening with the first Noise-only wire generation and observable-flow diversification. Client and server must be upgraded together.

## Noise-only cryptography

- The previous custom handshake/KDF/record crypto path is removed.
- Every session uses `Noise_NKpsk0_25519_AESGCM_SHA256`.
- Existing client ID + 32-byte PSK + server X25519 public key provisioning is reused.
- The client ID is bound into the Noise prologue and the session ID is authenticated inside handshake message 2.
- Fresh responder and initiator ephemeral X25519 keys provide an `ee` contribution and forward secrecy after ephemeral key destruction.
- Transport payloads use the Noise `Split` cipher states directly; there is no second home-grown transport-key derivation.
- One HTTP logical record can carry multiple <=65535-byte Noise messages while retaining MBox's logical record counter/ACK/replay behavior.
- Old beta.1/hardened clients are intentionally rejected; there is no legacy fallback/downgrade.

The minimal vendored Noise implementation is restricted to this one suite and is tested byte-for-byte against an independent NKpsk0/AES-GCM/SHA-256 vector.

## Reliability

- A committed DOWN batch is retained as the exact encrypted record until the client acknowledges its contiguous logical counter.
- Retrying the same DOWN request sequence after a lost HTTP response replays byte-identical ciphertext.
- GET polling and POST piggyback use one serialized DOWN commit path.
- Client counter acceptance/reorder/frame delivery run through one delivery goroutine.
- UP is bounded/chunked and scheduled fairly across streams.
- Per-stream WINDOW credits and bounded writer queues provide backpressure and slow-stream isolation.

## External flow diversification

- Random padding range is the default instead of power-of-two padding classes.
- HTTP batch limits vary within a configurable range.
- Noise transport messages use random chunk boundaries within a logical HTTP record.
- Active polling/backoff uses configurable ranges and jitter.
- Randomness is chosen before record commit; an HTTP retry always reuses the exact committed ciphertext and therefore cannot cause nonce reuse.

This reduces deterministic size/timing fingerprints but does not hide Go's TLS/HTTP fingerprint. Browser/Chromium networking remains a later task.

## Subscription controls and small VPS operation

Profiles support fixed/server-default/unlimited concurrent-session limits, explicit expiry and renewal without recreating credentials. Defaults target low-cost 1-vCPU/1-GiB servers with a bounded 64-MiB global queued-byte budget and `mboxctl resources` visibility.

## REG.RU / sing-box

The supplied `Private Rooms` HTML is an optional decoy-site example on the shared-host frontend, not the origin VPS. Administrators may replace it and configure their own transport API paths.

`127.0.0.1:2080` is the documented local sing-box `mixed` handoff; sing-box decides whether traffic exits directly or chains to another server/protocol.

## Android

Profile replacement is transactional, displays the server public-key SHA-256 fingerprint and persists desired VPN running/stopped state. The beta.2 APK embeds the Noise-only Go core and therefore must be rebuilt/installed together with the server update.

## Known network compatibility issue

Field test on **2026-11-02**:

- unrestricted Wi-Fi was used only as a baseline and worked; white-list mode was not active on that Wi-Fi network;
- MTS mobile data worked while white-list restrictions were active in that test;
- MegaFon mobile data did not provide a usable MBox connection while white-list restrictions were active in that test.

This is a single field observation, not a universal carrier claim. Filtering may differ by region, APN, subscriber profile and time. See `docs/NETWORK_COMPATIBILITY.md`.

## Release gates still outstanding

beta.2 is not declared production/security-audited. Real 24-hour/7-day soak, Wi-Fi↔LTE chaos, 10/50/100-client load qualification and an external security review remain required before a production claim.
