# MBox v1.0.0-beta.4.1 — prerelease notes

[Русский](RELEASE_NOTES.ru.md) · [Android](docs/ANDROID_BETA4_1.md) · [Test evidence](docs/BETA4_1_STATUS.md)

**Android 10+; ARM64; package `io.mbox.android`; versionCode 8.** Production-signed prerelease is built by the owner from the reviewed Git commit. This file is **not** proof that a GitHub release or the final APK already exists.

## Fixed compared with beta.4

- Local SOCKS readiness no longer substitutes for remote liveness: authenticated existing Noise `framePing`/pong is checked before reporting CONNECTED and periodically after that, including wake/Doze events.
- Android separately checks the QUIC gateway and HEV while monitoring the Go core; transport reconnect normally retains the TUN, with bounded backoff and Auto fallback.
- Explicit VERIFYING/RECOVERING and finer connection stages in the app/notification. Native QUIC race CI and TLS integration tests added, release APK structural gates improved.
- Operator-only QUIC CA/leaf rotation tooling and documentation supplied; **not automatically installed on VPS**.

**Compatibility:** no MBox Noise/wire change; beta.4 server, existing HTTPS-only and QUIC profiles remain supported subject to the existing certificate lifetime. Auto may break existing streams; it does not do seamless QUIC↔HTTPS migration. QUIC uses HTTP/3 reliable streams, not DATAGRAM; app UDP over HTTPS still travels over TCP.

## Evidence and known limits

Maintainer reports five successful beta.4.1 functional checks: connection, screen-off idle, Wi-Fi/mobile handover, total loss of network and Auto. Go client/server and native QUIC race tests passed; Android debug APK built. Final commit CI, signed-release smoke test, controlled hung-gateway tests, physical packet capture (IPv4/IPv6/DNS/TCP/UDP), Lockdown, long soak and browser fingerprint comparison **remain unverified** as of the recorded report. No independent security audit; no guarantee of no leaks or universal DPI camouflage. See [testing](docs/TESTING.md) and [limits](docs/TRAFFIC_CAMOUFLAGE_LIMITS.md).

The test QUIC TLS certificate expires **2026-12-07**. Replacing a pinned self-signed leaf requires an authenticated profile trust migration; never disable certificate verification. See [operator runbook](docs/QUIC_TLS_ROTATION.md).

## Actual release artifact — populate from FINAL signed APK only

- Filename: `MBox-1.0.0-beta.4.1-android-arm64.apk`
- APK SHA-256: **Published alongside the signed APK as a `.sha256` release asset; verify the exact artifact**
- APK signer SHA-256: **Verified out of band against the existing release signing certificate**
- Source: **the Git tag `v1.0.0-beta.4.1`; verify its commit against GitHub CI**
- Signed-release device tests: **see the release checklist and published evidence; do not infer success from this source file**

```bash
sha256sum dist/release/MBox-1.0.0-beta.4.1-android-arm64.apk
MBOX_RELEASE_SIGNER_SHA256='<trusted-existing-pin>' \
  ./tools/check-production-apk.sh dist/release/MBox-1.0.0-beta.4.1-android-arm64.apk
```

Publish a GitHub **prerelease** only after the [checklist](RELEASE_CHECKLIST.md) is reviewed. Do not attach a debug or unsigned APK. Never publish private `mbox://` links, PSK, private server keys or raw sensitive logs.
