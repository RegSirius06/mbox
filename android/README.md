**beta.4.4:** on-demand QUIC DATAGRAM for application UDP; no periodic PING, hidden three-way UDP developer selector; subscription v2 country profiles. See [DATAGRAM](../docs/BETA44_DATAGRAM_EXPERIMENT.md). Production-signed APK requires original certificate and versionCode 11.

# MBox Android 1.0.0-beta.4.4

[Русский](README.ru.md) · [User guide](../docs/GETTING_STARTED.md) · [Build and signing](../docs/RELEASE_ENGINEERING_BETA43.md)

**Android 10+ (API 29), ARM64.** Once published, install the production-signed APK from [GitHub Releases](https://github.com/RegSirius06/mbox/releases), import an administrator-issued `mbox://` profile or HTTPS subscription, and tap Connect.

The app supports **AUTO (QUIC → HTTPS)**, English/Russian UI, Android system light/dark mode, multi-subscription management, a Disconnect notification action, and hidden diagnostics after seven taps on MBox. QUIC uses streams (**not** DATAGRAM); application UDP over HTTPS is carried inside authenticated MBox records over TCP. Switching transports can interrupt active application streams.

**For developers on Arch:**

```bash
cd ~/src/mbox-main
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
python3 -m pytest -q tests
GO111MODULE=off go test ./client ./server ./relay
cd android
./gradlew :app:assembleRelease
```

Gradle `assembleRelease` alone does **not sign** the production artifact. Run `./tools/build-release-apk.sh` from the project root with `MBOX_RELEASE_KEYSTORE`, `MBOX_RELEASE_KEY_ALIAS`, and `MBOX_RELEASE_SIGNER_SHA256` set to the existing production identity. Full commands, `apksigner`/checksum checks, and `adb install -r` are in [release engineering](../docs/RELEASE_ENGINEERING_BETA43.md).

**Never uninstall beta.4.2 merely to upgrade**; stored credentials and profiles may be lost. Do not publish debug APKs, personal test QUIC endpoints, or profiles containing credentials.
