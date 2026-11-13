# MBox v1.0.0-beta.4.1 — Android prerelease

**Android 10+ / ARM64 / `io.mbox.android` / versionCode 8.** Recovery-focused maintenance release for the beta.4 QUIC and HTTPS transports.

**Changes:** authenticated Noise liveness checks; separate QUIC gateway and HEV monitoring; TUN-preserving reconnect; detailed CONNECTING / VERIFYING / RECOVERING states; improved native QUIC CI. The wire protocol, legacy HTTPS-only profile format and server API are unchanged. The server does not require updating solely for heartbeat.

**Testing:** maintainer reported 5 passing core connection/recovery field scenarios; Go and native QUIC race tests passed and debug APK built. Independent IPv4/IPv6/DNS/UDP physical leak capture, complete Lockdown matrix, controlled dead-gateway tests, long soak and release-signed device acceptance are still pending. This is **not an independently audited no-leak or DPI-immune VPN**.

**Known limits:** Auto can reset existing streams; no QUIC DATAGRAM or multipath; HTTPS application UDP is encapsulated over TCP. The operator's old test QUIC certificate expires December 7, 2026; TLS CA/leaf rotation has **not** been deployed.

- **APK SHA-256:** `PENDING_FINAL_SIGNED_APK_HASH`
- **APK signer SHA-256:** `PENDING_VERIFIED_EXISTING_SIGNER_PIN`
- **Source commit:** `PENDING_FINAL_GIT_SHA`

Read [English release notes](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4.1/RELEASE_NOTES.md) or [русскую документацию](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4.1/RELEASE_NOTES.ru.md).

**Do not publish this draft with pending fields.** Attach only the owner-key production-signed APK and checksum, never debug/unsigned binaries, secret profiles or raw logs.
