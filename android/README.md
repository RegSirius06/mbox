# MBox Android

Android wrapper around the shared Go MBox client core and HEV tun2socks.

## Architecture

```text
Android apps -> VpnService/TUN -> HEV -> local MBox SOCKS -> MBox HTTPS transport -> shared-host frontend -> MBox server
```

The application package is excluded from its own VPN route. Before starting the Go core, Java resolves the public endpoint using Android's active physical `Network`; the Go transport dials that numeric address while preserving the URL host/TLS SNI. This prevents the MBox control connection from recursively entering its own TUN.

## Build

Pinned project tooling:

- Android SDK/API 36;
- Build Tools 36.0.0;
- Android Gradle Plugin 9.5.0;
- Gradle wrapper 9.6.0;
- Java 17;
- Go (CI pins Go 1.27.3).


### Arch Linux / Java 17

Gradle/AGP for beta.2 must be run with Java 17. On Arch, a newer system JDK (for example Java 27) can fail before Android compilation with `Unsupported class file major version 71`. Keep the system JDK if desired and select Java 17 only for this project:

```bash
sudo pacman -S --needed jdk17-openjdk
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
hash -r
java -version
./gradlew --stop
./gradlew --version
./tools/build-debug.sh
```

`./gradlew --version` should report a Java 17 launcher/daemon before building. You can also persist this for the project with `org.gradle.java.home=/usr/lib/jvm/java-17-openjdk` in `android/gradle.properties`; do not commit machine-specific SDK paths from `local.properties`.

Build an ARM64 debug APK:

```bash
./tools/build-debug.sh
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

`tools/build-core.sh` compiles the shared `../client` Go source as an Android ARM64 PIE named `libmbox.so` and copies it into `jniLibs`. There is no separate Android transport implementation. `useLegacyPackaging=true` is intentional: Android must extract the executable into `nativeLibraryDir`.

**beta.2 is Noise-only.** The APK embeds the same `Noise_NKpsk0_25519_AESGCM_SHA256` client core as the CLI. There is no legacy handshake fallback, so install the rebuilt beta.2 APK when the server is upgraded to beta.2. Existing provisioning PSK/client ID/server public key material remains usable.

The bundled HEV AAR is version 2.19.3; its expected SHA-256 is stored next to it.

## Profiles and confirmation

The app contains no production server domain/IP/base path/PSK/public key/client ID. Those arrive through a portable `mbox://` profile or deployment-specific HTTPS App Link.

Before replacement, the app displays the profile name, client ID, endpoint and SHA-256 fingerprint of the MBox server public key. Confirm the fingerprint through a trusted channel.

Replacement is transactional: key files are written into a new immutable generation first, then the active `client.json` is atomically moved into place. A crash while preparing a new profile therefore does not leave half-old/half-new key material.

Remote endpoints must use HTTPS. Plain HTTP is accepted only for loopback tests.

## Verified App Links / QR

For a deployment-specific build, supply the exact public provisioning hostname:

```bash
./gradlew -PmboxProvisioningHost=vpn.example.com assembleRelease
```

The same host must serve `/.well-known/assetlinks.json` containing the SHA-256 fingerprint of the APK release-signing certificate. Generate the JSON with:

```bash
mboxctl android assetlinks \
  --sha256-cert 'AA:BB:...:FF' \
  --output /path/to/webroot/.well-known/assetlinks.json
```

Generate provisioning URLs/QR:

```bash
mboxctl user app-link alice-phone
mboxctl user qr alice-phone --app-link
```

Raw `mbox://` links remain supported.

## Desired VPN state and recovery

The service persists whether the user wants the VPN running. If Android recreates a `START_STICKY` service after process loss with a null Intent, the persisted state controls whether the supervisor is restarted. Explicit Disconnect and `VpnService.onRevoke()` persist the stopped state.

The Go core performs bounded transport retry. The Android supervisor recreates the core/HEV/TUN stack with reconnect backoff while desired state remains enabled.

## Current limitations

- ARM64-only APK/core at present.
- MBox transports TCP; native UDP/QUIC tunneling is not implemented.
- One active profile is stored at a time.
- Browser TLS fingerprint impersonation is not enabled in beta.2; see `../docs/STEALTH.md`.
