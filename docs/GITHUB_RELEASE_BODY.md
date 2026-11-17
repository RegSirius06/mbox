# MBox v1.0.0-beta.4.3 — Android & Admin prerelease

**DRAFT — DO NOT PUBLISH UNTIL ALL PENDING FIELDS ARE FILLED.**

Android 10+ / ARM64 · `io.mbox.android` · `versionCode=10` · **Pre-release, not RC1**.

**New:** redesigned Android UI; Russian/English; Android system light/dark theme and corrected Connect/Disconnect contrast; custom MBox launcher icon; profile/subscription management; notification Disconnect; hidden diagnostics; GitHub update alerts; redesigned server web panel with optional HTTPS domain; interactive first-install shared-hosting setup.

**Connection:** QUIC is preferred, with HTTPS fallback via the configured ingress (such as REG.RU FastCGI). No intentional Noise/wire or provisioning format change; existing beta.4.2 client profiles should remain supported. Transport transitions may interrupt sessions. No QUIC DATAGRAM/multipath.

**Limitations:** no independently audited physical no-leak guarantee, no universal DPI bypass, REG.RU CPU metrics not integrated, Android packets/bytes/streams counters unavailable. Deployed origin version may differ from the app version until explicitly upgraded. Do not expose the admin panel without documented TLS and access controls.

**Artifacts:** production-signed APK only. Do not publish debug/unsigned APK, profiles, private subscription links, keystore or runtime secrets.

- APK: `MBox-1.0.0-beta.4.3-android-arm64.apk`
- APK SHA-256: `PENDING_SIGNED_APK_SHA256`
- Production signing certificate SHA-256: `17FD9F930B8356BECA8251DD215220771A3D020E56FB6A703B526C35CCCD7586`
- Git commit: `PENDING_FINAL_GIT_SHA`
- Final device test and server deployment status: `PENDING_FINAL_VERIFICATION`

[English release notes](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4.3/RELEASE_NOTES.md) · [Russian release notes](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4.3/RELEASE_NOTES.ru.md)
