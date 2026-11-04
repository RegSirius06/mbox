# Security

**Current beta.3 boundary:** the operator enabled `auth.require_authenticated_control=true` on the test RU VPS on 2026-11-05, and a new Android build continued to work through the REG.RU FastCGI relay. Official beta.2.1 Android lacks `cmac`, so it is incompatible in strict mode. This authenticates HTTP controls; it does **not** demonstrate Android physical-interface leak protection or exact browser fingerprinting.
 notes

[Русская версия](ru/SECURITY.md)

MBox is experimental and has not received an independent protocol/cryptography audit.

## Noise-only authentication and encryption

Every current session uses `Noise_NKpsk0_25519_AESGCM_SHA256`. Provisioning supplies a client ID, per-client 32-byte PSK and server static X25519 public key. Client ID is bound into the Noise prologue; fresh client/server ephemeral X25519 keys are generated per session. After `Split`, Noise transport cipher states protect MBox transport messages directly.

Exact HTTP retries retain original ciphertext. Reliable DOWN replay remains until the client acknowledges contiguous delivery, avoiding nonce reuse after a lost HTTP response. Record counters fail closed on exhaustion.

## OPEN hardening

Invalid/unauthorized OPEN returns the same generic camouflage failure rather than a protocol/version banner. Before expensive Noise work the server applies bounded concurrent-handshake and attempts-per-second limits. The server static private key is loaded once at startup and its file permissions are checked.

## Per-device stale-session supersession

Android stores a random stable per-installation `device_id`. The Go client places it **inside the authenticated Noise OPEN payload**, not in the public URL. When a new session for the same `client_id + device_id` is accepted, the server atomically supersedes stale sessions from that device. Other devices using the same account are not intentionally evicted. Legacy clients without a device ID retain legacy per-client-cap behavior.

This prevents airplane-mode/metro-style hard disconnects from filling the per-client session quota while the old server sessions wait for idle cleanup.

## Revocation and expiry

Removing/disabling/revoking a client, rotating its PSK, expiry, or reducing a session cap can terminate already-established sessions rather than waiting for reconnect. Global safety limits remain authoritative.

## Traffic camouflage / public fingerprint

The shared-host frontend is ordinary HTTPS and invalid probes do not receive an MBox banner. Size/timing/batching are diversified. Android now uses bundled Chromium Cronet for public HTTPS, removing the obvious Go TLS/HTTP implementation from the default public path. This is fingerprint reduction, **not exact browser impersonation or a security boundary**.

## Provisioning confidentiality and authenticity

New HTTPS App Links place the secret `mbox://` profile in the URL fragment instead of the query, reducing exposure to ordinary HTTP server logs. Legacy query-format links remain importable for compatibility.

This does not by itself solve provisioning authenticity: a compromised frontend able to replace the profile can still present malicious configuration unless the user verifies the server-key fingerprint through an independent trusted channel. A separate signed-profile/trust-anchor design remains open.

## Android loopback-bridge resource boundary

Android's Cronet bridge listens only on `127.0.0.1`, but other local apps
can still connect without knowing its random per-launch authorization token.
The pre-auth socket work is therefore bounded: at most 8 active workers and
32 pending connections. Excess connections are rejected and closed; shutdown
also closes queued sockets that never entered a worker. The 64-byte random
bridge token is validated before the path, headers or up to 8 MiB request body
are allocated/read; untrusted localhost peers cannot force pre-auth body
materialization. A host-JDK regression
test stresses overload and shutdown cleanup.

This prevents *this specific* unbounded worker-queue / socket accumulation,
but is **not** a proof against all Android FD, CPU, or denial-of-service attacks.
Physical-interface packet capture, Android lockdown, crash scenarios, and
long-duration memory/FD soak tests remain mandatory before release.

## Secret material

Treat as secret:

- `/etc/mbox/server.key`;
- `/var/lib/mbox/users.db`;
- backup archives;
- `mbox://` links and provisioning exports.

The server public key and its fingerprint are not secret.

## Repository policy

Never commit production keys, DBs, generated runtime configs or provisioning links. Git history persists even in private repositories.

## Audit boundary

Noise removes the largest home-grown cryptographic construction, but independent review is still required for framing/AAD/counters, retry/replay, provisioning, Android lifecycle and the Go<->Cronet bridge before any production-security claim.

## Credential admin socket security (post-beta.2.2 hardening candidate)

The on-host credential administration socket now bounds concurrent accepted
requests to 16; excess connections are closed instead of creating unbounded
goroutines. Server startup rejects an existing regular file or symlink at the
configured socket pathname; shutdown only removes actual sockets. Credential
JSON decoding rejects trailing objects/tokens after the first document. This
mitigates local availability and input-validation risks, but assumes the
admin socket directory and server host permissions remain trustworthy.

**Rollout:** This server change is source-only until tested and deployed. The
working origin is not modified by preparing this patch.
