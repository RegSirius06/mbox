# Security notes

MBox is experimental and has not received an independent audit.

## Current authentication and encryption

Authenticated-client OPEN v2 uses a client ID, per-client 32-byte PSK, ephemeral client X25519 key, and HMAC-SHA256. X25519 shared secret + PSK are fed into an HKDF-like HMAC-SHA256 construction with protocol/session/client domain separation. Separate 32-byte keys are derived for UP AES-256-GCM, DOWN AES-256-GCM, and server confirmation.

Records carry a direction and monotonically tracked counter. Retry logic preserves the exact ciphertext/counter for an uncommitted UP record; the server deduplicates committed counters.

## Important limitation: server static key

The server X25519 private key is static. The client key is ephemeral, but the current design must not be marketed as providing strong post-compromise forward secrecy if an attacker records traffic and later obtains the server static private key plus relevant PSK material.

A future protocol revision should prefer a well-reviewed Noise pattern or another established handshake rather than extending the custom handshake indefinitely.

## Secret material

Treat these as secrets:

- `/etc/mbox/server.key`;
- `/var/lib/mbox/users.db`;
- backup archives;
- `mbox://` links;
- provisioning exports.

The server public key is not secret.

## Repository policy

Production keys, DBs, links and generated runtime configs must never be committed, even to a private repository. Private repositories can later be shared/made public, and Git history is persistent.

## Timing camouflage

The current project randomizes client idle polling, wake delay, active jitter, and record size classes. This is not a proof of indistinguishability from ordinary web traffic. Traffic-analysis resistance should be treated as an empirical research topic.
