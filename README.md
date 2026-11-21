# MBox — a self-hosted Android VPN over QUIC and HTTPS

[Русский](README.ru.md) · [Install Android](docs/GETTING_STARTED.md) · [Run the server](docs/SERVER_QUICKSTART.md) · [Documentation](docs/README.md) · [Changelog](CHANGELOG.md)

**Release candidate: `1.0.0-beta.4.4`, Android `versionCode=11`.** Experimental **pre-release**, not RC1 or a claim of independent security certification. A GitHub release / APK is not considered published until the signed artifact, CI and final SHA are verified.

MBox uses Android VpnService/TUN with authenticated Noise (`Noise_NKpsk0_25519_AESGCM_SHA256`) and two outer **ingress transports**: direct QUIC/HTTP3 to a VPS gateway, or Chromium Cronet HTTPS/HTTP2 via an optional FastCGI web frontend. **AUTO transport** prefers QUIC and switches to HTTPS on failure. REG.RU is an optional ingress, not the exit country.

```text
                     +-- QUIC/HTTP3 --> VPS gateway ---+
Android VPN + Noise -|                                  +--> MBox origin --> sing-box SOCKS5 --> DK / KZ / other exit
                     +-- HTTPS/Cronet --> FastCGI ------+
```

**UDP in beta.4.4:** over QUIC, application UDP prefers a separate authenticated QUIC DATAGRAM lane. It is established only on real UDP demand, expires at idle, and can reattach to the same Noise session with a new authenticated AEAD epoch; **no periodic DATAGRAM PING**. While the lane is idle/connecting or unavailable, **AUTO UDP** uses reliable MBox streams. TCP and control traffic always use reliable streams. Under seven taps on the MBox title → Diagnostics → UDP routing, developers may select `AUTO` (default), `STREAMS only`, or `DATAGRAM only` (no UDP fallback; packets may be dropped). This developer option is deliberately hidden.

**Subscriptions and exits:** one stable secret HTTPS subscription URL may return multiple selectable profiles on Android beta.4.4. Subscription **v1** remains available for beta.4.3 (default exit); **v2** supplies per-profile authenticated `egress_id` for additional origin-controlled exits. MBD1/MBD2 are Noise OPEN payload formats, **not HTTP API version numbers**. Legacy clients use the configured default exit. Default and named exits are equally usable; the default exists for compatibility. Do not equate different exit countries with independent origin uptime.

**Compatibility:** operator-tested beta.4.3 subscription refresh and connection to the updated origin, and beta.4.4 Denmark/Kazakhstan routes via separate local SOCKS5 exits. This is **not** a guarantee for all earlier betas, hosts or networks. HTTPS works without QUIC; DATAGRAM is not needed for initial authentication.

## Get started

1. Obtain the **production-signed** `MBox-1.0.0-beta.4.4-android-arm64.apk` from [GitHub Releases](https://github.com/RegSirius06/mbox/releases) **after publication**. Android 10+, ARM64.
2. Import an administrator-issued `mbox://` profile or secret HTTPS subscription URL. APKs do not contain public VPN credentials.
3. Choose a profile, connect and approve Android VPN permission. With the same signing certificate use an in-place upgrade; **do not uninstall to update**.
4. For servers, see [first installation](docs/SERVER_QUICKSTART.md), [operations](docs/BETA44_OPERATIONS.md), [subscriptions and exits](docs/MBOX_SUBSCRIPTIONS.md), and [security](docs/SECURITY.md).

**Important limitations:** physical-device IPv4/IPv6/DNS/UDP leak capture, full Android Lockdown matrix, battery comparison, prolonged soak and REG.RU FastCGI CPU-budget tests remain open. Hosting may throttle/kill FastCGI workers; **the REG.RU CPU-risk issue is not fixed by DATAGRAM** because DATAGRAM bypasses that frontend. No promise of anonymity or universal filtering bypass. Never publish PSKs, `mbox://` links, subscription tokens, passwords, runtime configuration or keystore contents.

[Release notes](RELEASE_NOTES.md) · [Release checklist](RELEASE_CHECKLIST.md) · [All docs](docs/README.md) · [Source and issues](https://github.com/RegSirius06/mbox).
