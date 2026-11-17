# MBox — a self-hosted VPN using QUIC and HTTPS

[Русский](README.ru.md) · [Get connected](docs/GETTING_STARTED.md) · [Set up a server](docs/SERVER_QUICKSTART.md) · [Documentation](docs/README.md) · [Changelog](CHANGELOG.md)

**Current release candidate: `1.0.0-beta.4.3`.** This is an experimental prerelease, not RC1. The signed APK should not be described as published until final checks are complete and a GitHub Release exists.

## What is MBox?

MBox is an experimental VPN you can run on **your own server**. The Android app creates an Android VPN interface and carries application traffic through an encrypted connection. You control the server, access credentials, and users rather than relying on someone else's VPN subscription.

You need an **administrator-issued profile** or an **HTTPS subscription URL**. Installing the APK alone does not provide a working VPN.

MBox supports two outer transports:

- **QUIC:** direct UDP connection to the configured origin when the network allows it.
- **HTTPS:** a TCP-based connection via an ordinary HTTPS frontend (for example, a REG.RU FastCGI shared-host site). This is a **backup ingress path**, not necessarily a second VPN server. Both paths can lead to the same origin.

In **AUTO**, QUIC is preferred and HTTPS is attempted immediately after a QUIC failure. QUIC attempts are held off for 60 seconds following a failure; network changes reset the preference. Transport transitions can interrupt existing application connections; seamless multipath is not implemented.

```text
Android (MBox) --QUIC/UDP---------------------> origin VPS ----> Internet / configured exit
       |                                           ^
       +--HTTPS/TCP--> shared hosting FastCGI -----+
```

**Security limitations:** MBox is not an anonymity guarantee, does not promise connectivity in every network, and has not received an independent physical-network leak audit. The Android VPN is designed to fail closed, but you must validate this on the device, particularly with Always-on and Lockdown.

## Connect on Android

1. Use an **Android 10+ / ARM64** device.
2. Once published, obtain the **production-signed** `MBox-1.0.0-beta.4.3-android-arm64.apk` from [GitHub Releases](https://github.com/RegSirius06/mbox/releases). Avoid third-party debug builds.
3. Ask an administrator for a `mbox://` profile or HTTPS subscription URL. Treat either as a **secret**.
4. In MBox, open Settings → Profiles to import a link or use the clipboard import shortcut.
5. Tap **Connect**, accept the Android VPN permission, and check for **Connected**.
6. Disconnect in the app or from the persistent VPN notification.

An APK signed with the **same production certificate** can be installed in place over beta.4.2, preserving app data. Never uninstall simply to upgrade; stored credentials may be lost. See the [Android user guide](docs/GETTING_STARTED.md).

## Run your own server

You need a Linux VPS, SSH administration access, and, depending on the selected path, domain/TLS configuration. The REG.RU HTTPS frontend is **optional** if direct QUIC meets your needs; it can serve as an alternate ingress.

[First-install guide](docs/SERVER_QUICKSTART.md) · [Web administration](docs/ADMIN.md) · [Optional panel domain](docs/MBOX_ADMIN_PUBLIC_ACCESS.md) · [Shared hosting](docs/REG_RU_SHARED_HOSTING.md).

The interactive `./install.sh` targets **first-time origin installation**. Existing servers should use explicit modes such as `--check`, `--with-admin`, or `--with-subscriptions`. **`--upgrade-origin` restarts the origin and can disconnect users.** Never run it merely to update a displayed version label.

## Changes in beta.4.3

- Android screen redesign, profiles/subscriptions, Russian and English UI, system light/dark theme, readable Connect/Disconnect buttons, custom launcher icon, persistent notification with open/disconnect actions.
- Hidden diagnostics (seven taps on MBox). Reports connection state, transport, recovery attempts, and notification status. **Actual core packets/bytes/streams counters are not yet available.**
- GitHub Releases link and optional in-VPN new-version checks. When the VPN is down, no direct Internet fallback is used for update checks.
- Refreshed MBox Admin with SSH access and an **opt-in** HTTPS domain mode. Public exposure is disabled by default.
- Interactive setup with an explicitly confirmed REG.RU relay step; improved stale FastCGI worker handling during managed relay deployment.
- **REG.RU CPU monitoring is not implemented.** The UI does not contain live provider metrics.

Project source versioning is unified, but a VPS running the previous `mboxctl` remains beta.4.2 until you explicitly deploy an updated server component. Updating an HTML page does not update the origin.

## Documentation and support

[Historical beta.4 release status](docs/BETA4_RELEASE_STATUS.md) — legacy evidence, not a substitute for beta.4.3 acceptance.


[Release notes](RELEASE_NOTES.md) · [Security limits](docs/SECURITY.md) · [Tests](docs/TESTING.md) · [Release checklist](RELEASE_CHECKLIST.md) · [Changelog](CHANGELOG.md).

Source and releases: https://github.com/RegSirius06/mbox. Never post `mbox://` URLs, PSKs, HMAC secrets, signing keys, or private subscription links in issues, logs, or screenshots.
