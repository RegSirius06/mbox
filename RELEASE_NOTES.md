# MBox 1.0.0-beta.3 — prerelease notes (2026-11-05)

[Русская версия](RELEASE_NOTES.ru.md)

**Compatibility break with the published beta.2.1:** beta.3 requires authenticated HTTP control metadata (`cmac`) when the origin has `auth.require_authenticated_control=true`. The published beta.2.1 Android client does not send `cmac`. The Noise record suite and existing PSK/profile format have **not** changed. beta.2.2 builds containing `cmac` remain compatible. There is no fallback to unsigned controls in strict mode. The version bump itself does not add another wire incompatibility.

## What changed since the public beta.2.1

- Session-bound HMAC authentication of HTTP DOWN/UP/CLOSE control metadata; origin rejects unsigned requests in strict mode. The REG.RU FastCGI relay does not need to hold client PSKs.
- Incremental account/credential updates instead of copying the full credentials map on every user change.
- Limit concurrent origin HTTP requests; validate relay HMAC headers before body buffering; amortize bounded replay-nonce cache cleanup.
- Reject ambiguous, duplicated or encoded aliases of `seq`, `ack`, `max` and `cmac` control query parameters.
- Root-managed Arch-to-RU-VPS deployment helper with preflight, backup, executable digest verification and rollback.
- Android package version `1.0.0-beta.3`, `versionCode=6`; `mboxctl` and `VERSION` aligned. A public APK must be signed with a non-debug key and pass pinned-certificate verification.

## Field observations (one test deployment, 2026-11-05)

- Origin successfully updated on the root-managed RU VPS; backup/restore and `mboxctl doctor` passed, and the running executable digest matched the Arch build.
- New Android build installed over the prior build; MBox connected through unchanged REG.RU FastCGI, Telegram loaded and cellular Internet worked during active operator allow-list restrictions.
- Android reportedly resumed VPN after reboot; `auth.require_authenticated_control=true` was enabled on origin and the updated client continued to transfer traffic.
- These are operator observations for one deployment, **not** evidence of universal operator compatibility, lack of traffic leaks or 24/7 stability. Carrier not specified for this test.

## Known limitations and remaining release gates

- The encrypted tunnel transports TCP streams only. SOCKS UDP ASSOCIATE/BIND are rejected; native UDP is not implemented. UDP-dependent apps may fail, and there is no verified physical-interface leak-proof result.
- Physical Android Wi-Fi/LTE packet captures for DNS/TCP/UDP/IPv6 and process-crash/Always-on Lockdown remain required. Users should enable Android Always-on VPN and Block connections without VPN where available.
- Signed one-time enrollment, long-run soak/load tests, multi-origin HA and full Chrome/Cronet traffic fingerprint comparison are not complete.
- Cronet is a Chromium-derived HTTP stack, **not** evidence that MBox is indistinguishable from Chrome or reliably bypasses every DPI/allow-list policy.
- REG.RU host is a separate shared-hosting FastCGI frontend, unchanged by the beta.3 version-only patch. Check current provider terms before new deployments.

This is an **experimental prerelease**, not a production-security certification or closure of all 55 hardening requirements. See `docs/BETA_3_RELEASE_STATUS.md` and `docs/BETA_2_2_HARDENING_MATRIX_RU.md` for the continuing open items.

---

## Historical beta.2.1 notes

# MBox 1.0.0-beta.2.1

[Русская версия](RELEASE_NOTES.ru.md)

`1.0.0-beta.2.1` is a beta.2-compatible hardening release. It keeps the beta.2 Noise wire protocol and existing credentials while adding Cronet outer HTTPS, network diagnostics, dual-stack/handover hardening, per-device stale-session supersession and calibrated resource defaults. `v1.0.0-beta.2` remains the previous tagged baseline. See `CHANGELOG.md` for the full delta.

## Why the public fingerprint is less obvious

The released beta.2 Android path exposed Go `crypto/tls` + `net/http` on the public HTTPS connection. The current Android default delegates the outer HTTPS request to bundled Chromium Cronet over an authenticated loopback bridge, while the Go core still owns Noise/MUX/replay/ACK. Real-device logs confirmed HTTP/2 negotiation (`h2`) and successful Noise/tunnel establishment through this path.

For a carrier, this replaces an obvious Go implementation signature with a coherent Chromium-derived application networking stack. It should be treated as a **fingerprint reduction**, not as exact Chrome impersonation or proof that any particular DPI/allow-list system will accept the traffic. QUIC remains disabled for the first rollout.

## Real-device reliability checks completed

- unrestricted Wi-Fi Cronet/HTTP2/Noise/tunnel baseline;
- normal Telegram, Instagram and video traffic;
- Wi-Fi/mobile handover;
- airplane-mode / metro-style hard disconnect and recovery;
- wait-for-underlying-network behavior without blind retries while the radio is unavailable;
- same-device session supersession on the server, preventing stale sessions from filling the per-client session cap;
- stream cleanup observed after load; default `max_streams_per_session` is now 128 after a stress run hit the previous per-session ceiling.

## Mobile white-list status

No fresh white-list-mode field test has yet been performed with this current branch. The relay/origin architecture and beta.2 MBox wire semantics are preserved, so networks that previously carried the setup are expected to remain compatible, but this still needs field confirmation.

MegaFon is specifically **unknown** for the current build. The next controlled experiment is the same REG.RU shared-host frontend/backend with `.fun` and `.ru` hostnames, tested in Chrome and in MBox/Cronet. A `.ru` domain may receive different operator treatment, but this is only a hypothesis until measured.

---

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
