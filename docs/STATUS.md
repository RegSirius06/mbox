# MBox release status

## 1.0.0-beta.2

beta.2 is the first Noise-only MBox generation. Client and server are wire-incompatible with beta.1/hardened and must be upgraded together.

Automated/repository gates in the release tree include:

- independent `Noise_NKpsk0_25519_AESGCM_SHA256` vector reproduction;
- Noise-only OPEN interoperability and explicit legacy-OPEN rejection;
- multi-message Noise transport roundtrip and authenticated-header tamper rejection;
- configuration upgrade/default backfill;
- SQLite user/policy control plane;
- reliable DOWN replay/ACK/order regression tests;
- bounded UP/flow-control regression tests;
- `go test -race` for Noise/server/client/relay;
- parser fuzz smoke;
- 3 GiB bounded-UP scheduler regression;
- slow-stream/full-duplex isolation regression;
- Android Go-core cross-build;
- generated REG.RU relay/site bundle;
- sing-box example JSON validation;
- secret/release-tree review.

Deployment behavior includes:

- Noise-only NKpsk0 handshake and Noise transport AEAD; no legacy downgrade;
- stochastic padding/batching/Noise-message boundaries/poll timing;
- unprivileged `mbox` systemd service;
- configurable low-memory limits and `mboxctl resources`;
- concurrent-session limits/unlimited mode;
- expiry + renewal without deleting credentials;
- active session revocation on policy changes;
- transactional Android profile replacement/fingerprint confirmation;
- optional Verified App Link provisioning;
- optional shared-hosting decoy-site deployment;
- explicit sing-box `127.0.0.1:2080` routing handoff.

Known beta limitations/gates:

- TCP transport only; native UDP/QUIC tunneling is not supported.
- Native Go TLS/HTTP fingerprint remains observable; browser-stack impersonation is not yet implemented.
- Per-user "device" limiting currently means concurrent authenticated sessions, not immutable hardware identity.
- 24-hour/7-day soak, network-transition chaos and 10/50/100 real-client qualification are still required before production status.
- Long-running fuzz campaigns remain part of ongoing hardening.
- Verified App Links require deployment-specific release signing + `assetlinks.json`.
- Noise substantially improves the protocol construction but the complete MBox system has not received an independent external security audit.
