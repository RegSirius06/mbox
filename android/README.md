# MBox Android

Minimal Android wrapper around the MBox client core and HEV tun2socks.

## Architecture

Android apps -> VpnService/TUN -> HEV -> local MBox TCP endpoint -> MBox HTTP transport -> server.

The application package is excluded from its own VPN route. The native Go process
therefore reaches the public MBox endpoint through the physical network instead of
looping back into HEV.

## Build

Requirements: Go, Android SDK 36, Java 17+ compatible with the selected Gradle/AGP,
and an arm64 Android device.

```bash
./tools/build-debug.sh
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

`tools/build-core.sh` compiles `core/*.go` as an Android arm64 PIE named
`libmbox.so` and copies it into `jniLibs`. `useLegacyPackaging=true` is intentional:
Android must extract that file to `nativeLibraryDir`, where executing it is allowed.

The bundled HEV AAR is version 2.19.3. Its expected SHA-256 is stored next to it.

## Profiles

The app contains no server domain, IP, base path, PSK, public key or client ID.
Those arrive through a portable `mbox://` link and are stored in the app-private
profile directory.

Remote endpoints must use HTTPS. Plain HTTP is accepted only for loopback tests.

## Current limitations

- Android package is arm64-only because the MBox Go executable is currently built only for arm64.
- MBox currently transports TCP/SOCKS CONNECT. Native UDP is not implemented in the MBox core yet.
- One active profile is stored at a time.
- A lost server session terminates the core and tears down the Android VPN cleanly; automatic re-handshake/reconnect is a later feature.

## Recovery behavior

The Go core retries a broken active transport with exponential backoff and exits after 30 seconds without a successful HTTP response. The Android service supervises the core: while the user still wants the VPN enabled, it tears down the dead HEV/TUN instance and recreates the whole tunnel with a 1, 2, 4, 8, 16, 30 second reconnect backoff. Pressing Disconnect cancels the supervisor and prevents automatic restart.
