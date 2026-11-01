# Security notes

MBox is experimental and has not received an independent protocol/cryptography audit.

## Noise-only authentication and encryption

MBox 1.0.0-beta.2 uses `Noise_NKpsk0_25519_AESGCM_SHA256` for every session. The previous custom X25519/HMAC/HKDF handshake and its record-key derivation are removed from the client and server; there is no legacy negotiation or downgrade path.

Provisioning supplies a client ID, per-client 32-byte PSK and the server static X25519 public key. The client ID is bound into the Noise prologue. Fresh client and server ephemeral X25519 keys are generated for each session, and the authenticated session ID is returned inside Noise handshake message 2.

After Noise `Split`, MBox uses the resulting Noise transport cipher states directly. One logical HTTP record may contain multiple <=65535-byte Noise transport messages. Direction, logical counter, Noise nonce range, chunk metadata and session ID are authenticated. See `docs/NOISE.md`.

Exact HTTP retries retain the original ciphertext. DOWN ciphertext remains replayable until the client acknowledges contiguous delivery, preventing nonce reuse caused by a lost HTTP response.

## Forward secrecy and key compromise

Noise NK includes an ephemeral-ephemeral (`ee`) DH in its second message. Once session ephemeral private keys are destroyed, later disclosure of the responder static private key and the client PSK alone does not reconstruct past transport keys. This is a substantial improvement over the removed custom handshake, which depended on client-ephemeral/server-static DH only.

Compromise of a live endpoint can of course expose live session state and long-term credentials. Protect `/etc/mbox/server.key`, `/var/lib/mbox/users.db`, backups and provisioning links.

## Revocation and expiry

Auth/policy reload tracks client ID and the PSK identity used by each active session. Removing/disabling/revoking a client, rotating its PSK, expiration, or reducing a session cap can terminate already-established sessions rather than waiting for reconnect.

## Traffic camouflage

The shared-host frontend is a real HTTPS site and invalid probes do not receive a distinctive MBox banner. beta.2 additionally makes HTTP batch sizes, Noise-message boundaries, padding and active polling/backoff less deterministic.

This is flow-shape diversification, **not browser TLS impersonation**. The Go client still exposes Go's native TLS/HTTP stack characteristics. Browser/Chromium transport work remains a separate next step; see `docs/STEALTH.md`.

## Secret material

Treat these as secrets:

- `/etc/mbox/server.key`;
- `/var/lib/mbox/users.db`;
- backup archives;
- `mbox://` links and provisioning exports.

The server public key and its fingerprint are not secret.

## Repository policy

Production keys, DBs, links and generated runtime configs must never be committed, even to a private repository. Git history is persistent and private repositories can later be shared.

## Audit boundary

Using Noise removes the largest home-grown cryptographic construction, but it does not make MBox security-audited. Independent review is still required for the Noise integration, MBox framing/AAD/counter rules, retry/replay logic, provisioning and Android lifecycle before a production security claim.
