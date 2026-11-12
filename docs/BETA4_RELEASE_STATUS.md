# MBox beta 4 — verified release-candidate state

[Русский](ru/BETA4_RELEASE_STATUS.md) · [Release notes](../RELEASE_NOTES.md)

**Source version:** `1.0.0-beta.4`. A signed ARM64 APK has been built, checked and installed on Android. **That does not establish complete real-device acceptance of the signed release APK or independent security assurance.** Its known file SHA-256 and signer fingerprint are in the canonical [release notes](../RELEASE_NOTES.md). GitHub publication is handled separately from local build/install.

Current architecture: Android `VpnService` → Noise/MBox → either QUIC/HTTP3 streams over external UDP to a VPS gateway or Cronet HTTPS/HTTP2/TCP via FastCGI. Auto prefers QUIC, then reconnects over HTTPS. QUIC endpoint and X.509 trust are configured **per profile**. UDP inside HTTPS stays inside TCP. No multipath or QUIC DATAGRAM.

Maintainer observed on debug: Wi-Fi QUIC, MTS/MegaFon HTTPS, Auto/reconnect and Telegram. Tests: 110 Python passed; release build/install/signature verified. Release transports on a second phone/Tele2, real-device packet capture, long soak and independent audit remain pending. Test QUIC certificate expiry: 2026-12-07.
