> This describes beta.4; for candidate beta.4.1 see [Android recovery](ANDROID_BETA4_1.md).

# MBox beta 4 on Android — user guide

[Русский](ru/ANDROID_BETA4.md) · [Transports](BETA_4_TRANSPORTS.md) · [Issues](https://github.com/RegSirius06/mbox/issues)

**Android 10+ (API 29), ARM64.** The beta 4 release APK has been built and installed by the maintainer. This does not imply independent full security or field acceptance.

## Connect

1. Download the APK from [GitHub Releases](https://github.com/RegSirius06/mbox/releases) once the prerelease is published. Verify its SHA-256 and the publisher certificate from the [release notes](../RELEASE_NOTES.md).
2. Launch MBox and import the private `mbox://` link supplied by your administrator via **Settings → Paste profile link** or **Import from clipboard**. Never share the link: it may include long-lived credentials.
3. Choose **Auto**, press **Connect**. The status reports the active transport. Check that new websites load and show the expected exit IP; a VPN Connected badge alone does not prove DNS/UDP functionality.

## Modes

- **Auto:** QUIC first if configured, HTTPS on QUIC failure. Network changes trigger re-evaluation. Auto is reconnect fallback, not seamless handover or multipath.
- **QUIC:** external UDP/HTTP3 streams directly to a VPS gateway; needs `transport.quic_url` and public `transport.quic_ca_pem` in the profile.
- **HTTPS:** Cronet HTTP/2 over TCP to a reachable frontend (optionally REG.RU shared hosting/FastCGI). Application UDP can travel inside encrypted MBox over TCP. HTTPS-only profiles still work.

In release, there are **no embedded QUIC debug endpoint/certificate assets**. A profile missing QUIC fields is not proof that UDP is blocked by Wi-Fi: provide a valid QUIC-enabled profile first.

## Protection and updates

Enable **Always-on VPN** and **Block connections without VPN** (Lockdown) in Android system VPN settings; test on your device. This does not substitute for independent physical-interface leak capture, which remains incomplete. Do not publish raw logcat containing addresses or secrets.

Release and debug builds have different signing certificates. `INSTALL_FAILED_UPDATE_INCOMPATIBLE` means Android refused an in-place change of signer. **Save the private profile securely before any uninstall**; uninstall removes app data. Future release updates should use the same permanent certificate.

If HTTPS appears while Wi-Fi works, Auto may have fallen back after a QUIC failure; test manual QUIC and inspect sanitized `adb logcat -d -s MBox:I` for `TRANSPORT` and `QUIC_FAILED`. Do not assume that "gateway ready" means the Go core established its SOCKS listener.

The test QUIC server certificate expires **2026-12-07** and must be renewed by the operator, together with updated trusted profiles. Do not disable TLS validation to fix it.

## Report a problem

Include Android version, device, mobile carrier/Wi-Fi, mode, sanitized status/error lines and reproduction steps. Do not attach a full `mbox://`, keys, bearer tokens or private server config. [GitHub Issues](https://github.com/RegSirius06/mbox/issues).
