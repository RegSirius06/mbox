# Changelog

## 1.0.0-beta.2

Noise-only security, reliability, low-resource operation, subscription control and flow-diversification beta.

Highlights:

- replaced the custom handshake/KDF/record crypto with `Noise_NKpsk0_25519_AESGCM_SHA256` and removed legacy downgrade support;
- verified Noise interoperability against an independent vector;
- added fresh responder ephemeral DH/forward secrecy and direct Noise transport cipher-state use;
- reliable DOWN request sequence / ACK / exact encrypted replay after lost HTTP responses;
- strict serialized DOWN ordering across GET polls and POST piggyback delivery;
- bounded/chunked fair UP scheduling and per-stream WINDOW flow control;
- stochastic padding, batch sizes, Noise-message boundaries and active poll timing;
- bounded writer queues, deadlines, rate/session/stream/memory quotas;
- immediate active-session revocation after disable/revoke/PSK rotation/expiry;
- per-profile concurrent-session limits, unlimited mode, expiry and renewal without recreating credentials;
- configurable low-memory server limits and `mboxctl resources`;
- unprivileged hardened systemd service and reduced journal write volume;
- real backup test-restore/integrity validation and upstream health checks;
- transactional Android provisioning with server-key fingerprint confirmation and persisted desired VPN state;
- optional Verified App Links/QR provisioning support;
- packaged REG.RU decoy site with explicit opt-in deployment and configurable API paths;
- documented sing-box `127.0.0.1:2080` routing handoff;
- race/fuzz/replay/order/full-duplex/slow-stream/multi-GB transport tests and expanded CI.

Browser TLS impersonation is deliberately not claimed in beta.2; native Go TLS/HTTP characteristics remain visible. See `docs/STEALTH.md`.

## 1.0.0-beta.1

First beta release of MBox.

Highlights:

- multiplexed encrypted transport over non-streaming HTTP intermediaries
- authenticated per-device provisioning
- retry, deduplication, buffering and frame reordering
- Android VpnService client with automatic reconnect
- SOCKS integration and downstream Hysteria2 routing
- SQLite-backed user management with `mboxctl`
- REG.RU FastCGI relay generation and deployment tooling
- server-originated optional cover traffic
- safe installer/update/migration workflow
- destructive reinitialization with automatic backup
