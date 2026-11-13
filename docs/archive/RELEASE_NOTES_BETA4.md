# Historical beta.4 release notes (archive)

# MBox 1.0.0-beta.4 — prerelease

[Русская версия](RELEASE_NOTES.ru.md) · [Android](docs/ANDROID_BETA4.md) · [Source](https://github.com/RegSirius06/mbox)

**Channel:** beta/prerelease. **Target:** Android 10+ (API 29), ARM64. APK: `MBox-1.0.0-beta.4-android-arm64.apk`, package `io.mbox.android`, `versionCode=7`. Users import a separate private `mbox://` profile. The APK does **not** embed a deployment-specific QUIC address or certificate.

## New in beta 4

- **Native QUIC/HTTP3 over external UDP** to the VPS gateway, using HTTP/3 streams rather than QUIC DATAGRAM.
- **HTTPS/Cronet** via a reachable frontend/FastCGI with application UDP encapsulated inside authenticated MBox records over TCP.
- **Auto** tries QUIC, falls back to HTTPS on failure and reevaluates the transport on network changes. HTTPS-only profiles remain valid.
- **Simplified Android UI** with connection control, status and Settings. Diagnostic lab features remain separate.
- Per-profile `transport.quic_url` (numeric IPv4 URL) and public X.509 `transport.quic_ca_pem`.
- The local QUIC Unix socket no longer requires developer mode; unsafe diagnostic flags remain prohibited in release builds.
- Signed release APK contains both `libmbox.so` and `libmboxquic.so`.

## Evidence and limits

- On Arch: **110 passed** in `pytest`, `assembleRelease` succeeded, APK manifest/signature and native libraries checked.
- APK installed successfully on Android as `1.0.0-beta.4` (`versionCode=7`).
- **Debug build** tested on real networks: Wi-Fi QUIC, restricted MTS/MegaFon HTTPS, Auto, reconnects, Telegram.
- These do **not** prove every feature works in the newly signed release build. Tele2 on a second device and comprehensive packet-capture leak testing remain pending.

## Artifact identity

Verified build APK SHA-256:

```text
c3b1cbcb1b4b472aba35237657c2834a2aa8f032d861cbf817f5a5b9f18b43e1
```

APK signing certificate SHA-256 (a different digest):

```text
17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586
```

Recheck and update the **APK** SHA-256 if the build is repeated; never publish a checksum for a different file. Keep using the same pinned signing certificate.

## Security and known limitations

- No independent security audit, complete DNS/IPv4/IPv6 physical leak capture or long soak. **No claim of leak-proof operation.**
- Android Always-on VPN and Lockdown must be enabled in OS settings and validated per device.
- Auto is reconnect failover, not multipath or seamless stream migration.
- A test QUIC certificate expires **2026-12-07**. Its operator must renew it and redistribute authenticated trust; never bypass TLS verification.
- Provisioning links are secrets. Android debug-signed APKs cannot be upgraded in place to the release-signed APK without a matching signer; back up the profile safely before uninstalling.

[Android guide](docs/ANDROID_BETA4.md) · [Issue tracker](https://github.com/RegSirius06/mbox/issues)
