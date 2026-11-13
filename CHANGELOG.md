## 1.0.0-beta.4.1 — prerelease candidate

- Authenticated Noise framePing/pong liveness with a deadline; Android VERIFYING and RECOVERING stages, gateway/HEV watchdog and TUN-preserving reconnect.
- Same server wire protocol and HTTPS-only profiles as beta.4; native QUIC CI and TLS tests. Operator TLS rotation tooling provided but not installed.
- Maintainer reported five passing connection/recovery scenarios; independent physical leak capture, signed-APK acceptance and long soak remain pending.

## 1.0.0-beta.4 — Android beta / 2026-11-13

- Direct QUIC/HTTP3 (external UDP to a VPS gateway), with MBox records inside HTTP/3 streams rather than QUIC DATAGRAM.
- Auto prefers QUIC and reconnects via HTTPS/Cronet on failure or network change; no multipath or seamless flow migration.
- Application UDP carried through authenticated/encrypted MBox frames on both HTTPS/TCP and QUIC paths; production `enable_udp` no longer depends on developer mode.
- Release Android ARM64 APK with a simplified UI, per-profile `transport.quic_url` + `transport.quic_ca_pem`, and pinned permanent signing identity.
- 110 Python tests passed, signed APK was built and installed. Wi-Fi QUIC and restricted MTS/MegaFon HTTPS verified on debug; comprehensive signed-APK field testing, Tele2, packet capture, and long soak remain open.
- The test QUIC certificate expires 2026-12-07; operator renewal and client trust updates required.

See [beta 4 release notes](RELEASE_NOTES.md).

## Unreleased: 1.0.0-beta.4 (draft)

- Forward application UDP through MBox, including UDP-over-HTTPS/TCP via REG.RU.
- Add a direct native QUIC/HTTP3 gateway (HTTP/3 streams, not QUIC DATAGRAM).
- Add Android Auto selection and QUIC → HTTPS fallback on reconnection.
- Harden the QUIC systemd unit with DynamicUser/NoNewPrivileges and validate reconnection.
- Field-test HTTPS, QUIC and Wi-Fi/LTE transition; report one successful MegaFon test.
- **Before publication:** include QUIC in the signed release APK and verify the installed release build. Root cause of the 2026-11-12 FastCGI failure remains unknown.

## 1.0.0-beta.3 — 2026-11-05 (experimental prerelease)

- **Breaking change vs published beta.2.1:** with strict `cmac` enabled on origin, the published beta.2.1 Android APK cannot authenticate HTTP UP/DOWN/CLOSE control requests. Noise records and PSK profiles are unchanged; beta.2.2 builds with `cmac` remain compatible.
- Session-bound `cmac` rejects forged ACK, counter, max and CLOSE mutations through untrusted HTTP frontend in strict mode.
- Incremental credential map updates avoid whole-map copies for single-user operations.
- P1 origin hardening: bounded concurrent requests; early relay HMAC-header rejection; amortized replay nonce TTL cleanup; strict canonical control query parser and fuzz checks.
- Reversible root-managed VPS deployment; release-signature pinned-certificate gate; Android `versionCode=6` / CLI/version alignment.
- Single-device field check under active mobile allow-list restrictions on 2026-11-05: connection and Telegram worked through unchanged REG.RU relay. No physical packet capture or multi-day soak was completed.
- Remains experimental TCP-only transport, not a completed security audit or claim of universal DPI compatibility. See `RELEASE_NOTES.md`.

## 1.0.0-beta.2.1 — 2026-11-02

This development branch keeps the beta.2 Noise wire generation and focuses on network robustness, diagnostics, a less obvious public-network fingerprint and real Android lifecycle behavior. It is not yet a tagged release.

### Network and Android reliability

- added stage-oriented diagnostics from DNS/TCP/TLS/ALPN/HTTP through MBox OPEN, Noise and tunnel establishment;
- added bounded A/AAAA fallback for the Go diagnostic stack and dual-stack IPv4/IPv6 Android TUN routing;
- fixed HTTP/2 enablement for the custom Go transport and added response/header bounds plus redirect denial;
- added non-VPN underlying-network monitoring, wait-for-network behavior and immediate reconnect on real Wi-Fi/mobile handover;
- added stable per-installation `device_id` authenticated inside Noise OPEN; a new session supersedes stale sessions from the **same device** after hard disconnects without evicting other devices using the same client account;
- raised the default per-session stream quota from 64 to 128 after a real-device Telegram/Instagram/YouTube stress run reached 64 concurrent streams while idle cleanup was observed to release them normally;
- added pre-auth OPEN concurrency/rate limits and more explicit OPEN rejection diagnostics.

### Public HTTPS / fingerprint

- Android public HTTPS now defaults to bundled **Chromium Cronet** through an authenticated loopback bridge; Noise, MUX, ACK/replay and flow control remain in Go;
- HTTP/2 is enabled and QUIC is intentionally disabled for the first rollout;
- added `cronet` / `go` A/B profile selection and deterministic camouflage mode for controlled captures;
- added fingerprint regression tooling that detects accidental fallback to the Go public HTTP stack;
- real-device validation confirmed Cronet + HTTP/2 + Noise + ordinary app traffic on unrestricted connectivity.

The goal is to remove the conspicuous Go TLS/HTTP implementation fingerprint and present a coherent Chromium-derived app networking stack. **This is not a claim of exact Chrome impersonation or universal DPI bypass.** A browser-vs-MBox packet fingerprint comparison is still an open qualification task.

### Security and provisioning

- server static Noise key is loaded once at startup and permission-checked;
- Noise counters fail closed at exhaustion;
- secret-bearing HTTPS provisioning moved from query parameters to URL fragments while legacy imports remain accepted;
- invalid/unauthorized OPEN continues to return generic camouflage responses; expensive pre-auth handshakes are now bounded.

### Field-test status

`1.0.0-beta.2.1` has **not yet been field-tested during active mobile white-list restrictions**. The public shared-hosting/FastCGI path and MBox wire semantics used by previously working deployments are preserved, so no compatibility regression is currently expected on networks where the older deployment already worked; this is an engineering expectation, not a fresh carrier result.

MegaFon remains unresolved. Historical observation showed the `.fun` frontend itself could fail on MegaFon before MBox was involved, while the same class of setup was reachable on MTS/T2. A controlled `.fun` vs `.ru` test on the same REG.RU shared-host backend/IP/TLS configuration is planned. A `.ru` domain is a hypothesis for broader operator allow-list treatment, not a guaranteed fix.

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
