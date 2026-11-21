# MBox v1.0.0-beta.4.4 — prerelease

**DRAFT — REPLACE ALL PENDING FIELDS BEFORE PUBLISHING.** Android 10+, ARM64, `io.mbox.android`, versionCode 11. **Pre-release, not RC1.**

## Changes

- On-demand authenticated QUIC DATAGRAM for application UDP; idle expiration and reattach using independent AEAD keys per epoch. No constant DATAGRAM PING. TCP/control still reliable streams.
- Hidden diagnostics UDP modes: AUTO (default), STREAMS only, DATAGRAM only. Android recovery/power and profile selection UI improvements.
- One stable subscription with v2 country profiles and authenticated origin `egress_id`; beta.4.3 retains v1/default-exit compatibility. Operator verified DK/KZ separate exits.
- Updated MBox Admin/installer safety, private SSH access, FastCGI build self-check and origin rollback flow. QUIC and HTTPS remain independent ingress paths.

## Limits / open issues

REG.RU FastCGI CPU/worker quotas are **not fixed**; keepalive-free DATAGRAM does not reduce CPU on HTTPS FastCGI traffic. Battery claims need controlled measurement. No independent leak/Lockdown or cryptography audit, no universal DPI promise. GitHub update alerts require a post-release test on beta.4.3.

**Assets:** production-signed APK only, never debug/profile/keystore/runtime secrets.
- APK: `MBox-1.0.0-beta.4.4-android-arm64.apk`
- APK SHA-256: `PENDING_SIGNED_APK_SHA256`
- Signing certificate SHA-256: `17FD9F930B8356BECA8251DD215220771A3D020E56FB6A703B526C35CCCD7586`
- Git SHA: `PENDING_FINAL_GIT_SHA`
- Signed APK field check / server status: `PENDING_FINAL_VERIFICATION`
