# MBox

[Русский](README.ru.md) · [Android](docs/ANDROID_BETA4.md) · [Changelog](CHANGELOG.md) · [Beta 4 notes](RELEASE_NOTES.md)

**Current version: `1.0.0-beta.4` (Android ARM64, Android 10+).** MBox is an experimental encrypted VPN transport with application UDP support and selectable outer transports. A beta is **not** an independently audited security product and is not guaranteed to work on every network.

## Android: three steps

1. Obtain `MBox-1.0.0-beta.4-android-arm64.apk` from [GitHub Releases](https://github.com/RegSirius06/mbox/releases). Check its SHA-256 and APK signing certificate against the [release notes](RELEASE_NOTES.md).
2. Install MBox and import your private `mbox://` profile from **Settings → Paste profile link** or **Import from clipboard**. The link contains credentials: never post it in an issue.
3. Select **Auto** and press **Connect**. Native QUIC requires the pair `transport.quic_url` and `transport.quic_ca_pem` in your profile. HTTPS-only profiles remain supported.

[Complete Android guide](docs/ANDROID_BETA4.md) · [Debug/release installation](android/README.md)

## Traffic paths

```text
                       +-- QUIC/HTTP3 over UDP --> QUIC gateway --+
Android VpnService --> |                                          +--> MBox origin --> sing-box --> exit
Noise/MBox client -----+-- Cronet HTTPS/HTTP2 --> frontend/FastCGI +
```

- **QUIC** uses external UDP but carries MBox over HTTP/3 **streams**, not QUIC DATAGRAM.
- **HTTPS** uses TLS/HTTP2/TCP, optionally through REG.RU shared hosting/FastCGI. Application UDP is encapsulated in MBox records over this TCP path; it is **not** direct external UDP.
- **Auto** tries QUIC first, then reconnects over HTTPS on failure. This is neither multipath nor seamless flow migration nor continuous latency racing.

## Security limitations

The Android client uses `VpnService`. Enabling Android **Always-on VPN** and **Block connections without VPN** (Lockdown) is recommended. Independent physical-interface DNS/IPv4/IPv6 leak tests, crash tests and long-duration soak tests are still incomplete. Signed binaries do not establish the absence of vulnerabilities. Private `mbox://` links contain reusable credentials.

See [security model](docs/SECURITY.md), [testing](docs/TESTING.md), and [known limits](docs/BETA4_RELEASE_STATUS.md). A deployment's test QUIC certificate expires **December 7, 2026**; its operator must renew the certificate and update client trust before that date.

## Operators and contributors

[Architecture](docs/ARCHITECTURE.md) · [Operations](docs/ADMIN.md) · [REG.RU FastCGI](docs/REG_RU_SHARED_HOSTING.md) · [Transports](docs/BETA_4_TRANSPORTS.md) · [Android release engineering](docs/ANDROID_RELEASE_ENGINEERING.md) · [Contributing](CONTRIBUTING.md)

Field reports: [GitHub Issues](https://github.com/RegSirius06/mbox/issues). Never attach credentials, full provisioning links, private host configuration, bearer tokens, or unsanitized packet captures.
