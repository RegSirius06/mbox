# MBox Android 1.0.0-beta.4

[Русский](README.ru.md) · [User guide](../docs/ANDROID_BETA4.md) · [Release engineering](../docs/ANDROID_RELEASE_ENGINEERING.md)

**Android 10+ (API 29), ARM64.** Debug and release share a simplified main screen. Debug-only laboratory diagnostics remain in Settings.

- **Auto:** QUIC preferred; HTTPS on QUIC failure, reevaluated after network changes.
- **QUIC:** direct external UDP/HTTP3, using streams (not DATAGRAM), with endpoint/certificate from the profile.
- **HTTPS:** Cronet HTTP/2/TCP via the configured frontend and FastCGI; application UDP inside encrypted MBox records over TCP.

Install the signed APK from the project's GitHub Releases. A debug APK with a different signer cannot be updated in place: save your secret profile *before* uninstalling. Per-profile `transport.quic_url` and `transport.quic_ca_pem` are needed for QUIC; HTTPS-only profiles remain valid. The release APK contains no deployment-specific QUIC address or certificate.

## Build on Arch

```bash
cd ~/src/mbox-main
./android/tools/build-debug.sh
export MBOX_RELEASE_KEYSTORE="$HOME/.local/share/mbox-signing/mbox-release.p12"
export MBOX_RELEASE_KEY_ALIAS=mbox-release
export MBOX_RELEASE_SIGNER_SHA256=17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586
./tools/build-release-apk.sh
```

Android `VpnService`/TUN/HEV forwards through the local Go core. Enable OS Always-on VPN and Lockdown, then test the behavior on your device. Independent physical-interface leak tests remain incomplete.
