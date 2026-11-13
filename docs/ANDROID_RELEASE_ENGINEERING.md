# Android release engineering — MBox 1.0.0-beta.4.1

> **beta.4.1:** Verify the final production-signed APK from the exact release commit. Do not reuse the beta.4 file checksum. Source/device tests are not a physical leak audit.

[Русский](ru/ANDROID_RELEASE_ENGINEERING.md) · [Checklist](../RELEASE_CHECKLIST.md)

Build and sign only on a trusted maintainer machine (Arch), with the permanent keystore stored **outside** the Git repository. Do not upload signing credentials or passwords.

- `VERSION=1.0.0-beta.4.1`, `android/app/build.gradle` `versionName=1.0.0-beta.4.1`, `versionCode=8`.
- `libmbox.so` and `libmboxquic.so` are packaged in **release** `arm64-v8a`, not only debug.
- `src/debug/assets/mbox-quic-remote.txt` and `mbox-quic-ca.pem` **must not** appear in the release APK. Runtime QUIC endpoint and X.509 PEM come from an imported `mbox://` profile.
- `enable_udp` is a production capability; developer-only network-debug/insecure options must not be enabled for normal release use.
- Release uses `applicationId=io.mbox.android`; signing certificate SHA-256 is `17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586` (publisher, not QUIC server).

```bash
cd ~/src/mbox-main
export MBOX_RELEASE_KEYSTORE="$HOME/.local/share/mbox-signing/mbox-release.p12"
export MBOX_RELEASE_KEY_ALIAS=mbox-release
export MBOX_RELEASE_SIGNER_SHA256=17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586
./tools/build-release-apk.sh
./tools/check-production-apk.sh dist/release/MBox-1.0.0-beta.4.1-android-arm64.apk
```

`build-release-apk.sh` refuses to overwrite an existing artifact: deliberately archive the previous APK before rebuilding. Any rebuild requires a **new** APK SHA-256 and updated release notes. Test the exact signed artifact (not just debug) on a real phone before publishing. `INSTALL_FAILED_UPDATE_INCOMPATIBLE` is expected when switching from debug signer; back up personal provisioning before uninstalling.

Independent physical DNS/UDP/IPv4/IPv6 leak tests and long soak remain open; operator QUIC test certificate expires **2026-12-07**.
