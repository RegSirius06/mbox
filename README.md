# MBox

[Русский](README.ru.md) · [Android](docs/ANDROID_BETA4_1.md) · [Security](docs/SECURITY.md) · [Changelog](CHANGELOG.md) · [Release notes](RELEASE_NOTES.md)

**Current release candidate: `1.0.0-beta.4.1`** — Android 10+ / ARM64 / `io.mbox.android`. This is an experimental encrypted VPN client, not an independently audited security or censorship-resistance product. GitHub release publication and final production-signed APK checks are separate steps.

## Get connected

1. Download the **production-signed** `MBox-1.0.0-beta.4.1-android-arm64.apk` from [GitHub Releases](https://github.com/RegSirius06/mbox/releases) **after the prerelease has been published**. Verify its actual SHA-256 and the signing-certificate fingerprint against the published release notes.
2. Import your private `mbox://` profile in Settings. Never publish or transmit that URL in an issue: it contains reusable credentials.
3. Choose **Auto** and tap **Connect**. The UI shows connection stages, the active transport, Noise verification, recovery and relevant failures. HTTPS-only beta.4 profiles remain supported.

## Transport and recovery

```text
Android VpnService / TUN / HEV -> MBox Go core / authenticated Noise
                               +-- QUIC/HTTP3/UDP -> native gateway -> MBox origin
                               +-- HTTPS/HTTP2/TCP -> Cronet -> optional REG.RU FastCGI -> MBox origin
```

- **QUIC:** direct UDP to the profile's VPS address. MBox application UDP travels through reliable HTTP/3 **streams**, not QUIC DATAGRAM.
- **HTTPS:** Cronet HTTP/2 over TLS/TCP through the configured endpoint (optionally shared-hosting FastCGI). Application UDP stays inside authenticated MBox records **over TCP**.
- **Auto:** prefers QUIC at connection time; falls back to HTTPS after confirmed failure. This is not multipath or seamless stream migration. Established connections may reset on transport replacement. There is no claim of continuous non-disruptive QUIC reprobe while HTTPS is healthy.
- **Liveness:** a session-specific authenticated Noise ping/pong is required for CONNECTED. The core also probes periodically (30 s with active streams, 90 s idle, jittered); Android requests an additional probe after wake/Doze exit and monitors the QUIC gateway and HEV.

## Security and test status

The maintainer reports passing connection, screen-off idle, Wi-Fi/mobile handover, total network outage and Auto checks on a beta.4.1 build. Go client/server and native QUIC race tests passed on Arch, and a debug APK built. **This does not prove the same matrix on the final production-signed APK**, nor that the original idle hang's physical root cause has been identified.

**P0 is not closed:** independent physical-interface IPv4/IPv6/DNS/TCP/UDP leak capture, Android Lockdown testing, controlled gateway failure, long soak and complete signed-APK acceptance are still pending. Always-on helps persistence; **Android Lockdown** is needed for OS-enforced traffic blocking when the VPN service itself is gone. No absolute no-leak, exact-browser-fingerprint or DPI-bypass guarantee is made.

The operator's test QUIC certificate expires **December 7, 2026**. TLS rotation tooling is supplied but **not deployed**; a one-time authenticated migration from pinned self-signed leaf to trusted CA may be necessary. Never turn off certificate verification.

See: [Android guide](docs/ANDROID_BETA4_1.md) · [Current status](docs/BETA4_1_STATUS.md) · [Test matrix](docs/TESTING.md) · [Camouflage limits](docs/TRAFFIC_CAMOUFLAGE_LIMITS.md) · [TLS rotation](docs/QUIC_TLS_ROTATION.md) · [Operations](docs/ADMIN.md).

Historical: [beta.4 release status](docs/BETA4_RELEASE_STATUS.md) is retained as an archive and does not describe this candidate.

Please report reproducible issues with sanitized diagnostic stages only; never provide a full `mbox://`, private keys or raw sensitive logs.
