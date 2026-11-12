# MBox status — 1.0.0-beta.4

[Русский](ru/STATUS.md) · [Release notes](../RELEASE_NOTES.md) · [Android](ANDROID_BETA4.md)

**Current source version:** `1.0.0-beta.4` (`versionCode=7`). The maintainer built and installed a signed ARM64 Android release candidate; the artifact passed manifest, library and signing-certificate checks. The candidate APK SHA-256 is recorded in the release notes. GitHub publication and independent security verification are **separate** steps.

**Observed debug field testing:** Wi-Fi QUIC, HTTPS on restricted MTS/MegaFon, Auto and reconnects, Telegram. **Release candidate testing:** signed APK installed and version confirmed; full transport acceptance on that artifact has not been logged. Tele2 second-device testing, 24–72h soak and independent physical leak capture are pending.

**Security scope:** Noise/MBox authenticated encryption, system Android VpnService, bounded transport paths, release signer pin; **not** a guarantee of no leaks or untraceability. Enable Always-on/Lockdown and test after deployment. QUIC test certificate expires on **2026-12-07**.

Historical beta.2/beta.3 engineering notes remain in Git history and dated audit documents. Do not treat old status text as a current implementation contract.
