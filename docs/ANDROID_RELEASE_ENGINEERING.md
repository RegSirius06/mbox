**beta.4.4:** production-signed ARM64 APK must have versionCode 11 and the original signing cert. Test `adb install -r` over an existing signed beta.4.3 and verify that profiles/Keystore survive. Hidden UDP modes are present in production release but not on the normal home screen. See [release checklist](../RELEASE_CHECKLIST.md).

# Android release engineering — MBox 1.0.0-beta.4.3

[Русский](ru/ANDROID_RELEASE_ENGINEERING.md) · [Complete release workflow](RELEASE_ENGINEERING_BETA43.md)

This page is intentionally short to avoid two conflicting signing instructions. The authoritative up-to-date commands and acceptance gates live in [MBox 1.0.0-beta.4.3 release engineering](RELEASE_ENGINEERING_BETA43.md).

**Release identity:** `io.mbox.android`, `versionName=1.0.0-beta.4.3`, `versionCode=10`, Android 10+ ARM64. The release APK must contain `lib/arm64-v8a/libmbox.so` and `libmboxquic.so` but **not** debug endpoint/certificate assets. Do not ship a debug build or unsigned APK.

Use the **original production PKCS12 signer** (do not rotate for a cosmetic release), keep it outside Git, and let `apksigner` prompt locally. The release script refuses overwriting existing APKs; archive previous outputs first. A successful Gradle build alone does not mean an APK was signed. Verify via `tools/check-production-apk.sh`, `sha256sum -c`, and an in-place Android device installation. Avoid making claims about physical no-leak certification or universally bypassing DPI.
