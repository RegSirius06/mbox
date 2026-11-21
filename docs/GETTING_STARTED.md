**beta.4.4:** Profile chooser displays country exit variants from a v2 subscription. Hidden developer UDP routing (seven MBox title taps) supports AUTO / STREAMS only / DATAGRAM only; keep AUTO for ordinary use. The app may check GitHub Releases through an active VPN and show a dismissible update notice; no direct network bypass.

# MBox Android: getting started

[Русский](ru/GETTING_STARTED.md) · [Home](../README.md)

## Requirements

An **Android 10+ / ARM64** device, the signed MBox APK, and a valid administrator-issued `mbox://` profile or HTTPS subscription link. **The APK does not itself provide a public VPN server.**

## Install and connect

1. After the release has been published, get `MBox-1.0.0-beta.4.4-android-arm64.apk` from [GitHub Releases](https://github.com/RegSirius06/mbox/releases). Until publication, this is the planned artifact name, not a live download.
2. Install the signed APK. Treat provisioning and subscription URLs as secrets.
3. Open MBox → Settings → Profiles. Import from clipboard or paste a profile/subscription URL. HTTPS subscriptions can expose more than one available ingress/profile.
4. Tap **Connect** and approve Android's VPN request. Wait for **Connected via QUIC/HTTPS**.
5. Disconnect using the app or the system VPN notification action.

## Connection states

- **Connecting / Verifying:** the client is establishing and authenticating the remote session.
- **Connected via QUIC:** direct UDP is working.
- **Connected via HTTPS:** the HTTPS ingress is in use, potentially through FastCGI shared hosting.
- **Recovering:** MBox is reconnecting; do not assume the tunnel is healthy until confirmation.
- **Failed / Waiting for network:** connectivity is unavailable or not yet restored.

**AUTO:** QUIC first, then immediately HTTPS after a failed QUIC attempt; subsequent QUIC attempts are held off for 60 seconds. Network changes reset the selection. A healthy HTTPS connection is not intentionally torn down just to return to QUIC. A transition can still interrupt calls or application streams; seamless multipath is not implemented.

## Languages, themes, diagnostics

MBox defaults to the phone language, with **English and Russian** choices. The interface follows Android's light/dark theme. Tap the MBox title seven times quickly to open read-only diagnostics. It shows state, transport, retry information and notification status. **Packets/bytes/streams core counters are not implemented yet.**

## Upgrade safely

Use a production-signed APK from the trusted source with the **same signing certificate**. Install over the existing version; via ADB: `adb install -r MBox-1.0.0-beta.4.4-android-arm64.apk`. Confirm that profiles and subscriptions persist. **Do not uninstall first**, or stored credentials can be lost.

## Troubleshooting and security

Check network access, profile validity/expiry, correct device time, and whether the chosen transport is available. AUTO can use HTTPS only when an HTTPS endpoint is configured. Do not share secret `mbox://` links, PSKs, private subscription URLs or sensitive logs. No universal DPI bypass or independent physical-network no-leak guarantee is claimed; test Android Always-on/Lockdown separately.
