# MBox v1.0.0-beta.4.3 — prerelease notes

[Русский](RELEASE_NOTES.ru.md) · [README](README.md) · [Checklist](RELEASE_CHECKLIST.md)

**Android 10+ · ARM64 · `io.mbox.android` · `versionCode=10`. Beta, not RC1.** Final release acceptance requires verification of the production-signed APK and independently staged server deployment.

## What's new

**Android:** redesigned navigation and home, Russian/English language selection, system-following light/dark theme, corrected primary-button contrast, custom MBox launcher icon. Dedicated profiles/subscriptions/settings screens, clipboard and manual import, confirmed local profile deletion, persistent notification with open/disconnect actions, and read-only diagnostics behind seven taps on MBox.

**Updates:** GitHub Releases link and a new-version notification with Stay in MBox / Open GitHub actions. Update checks do not bypass the VPN through a direct network fallback.

**Web administration:** redesigned dashboard and navigation for clients, subscriptions, configuration and diagnostics. SSH tunnel is recommended; an explicitly enabled HTTPS-domain mode with Caddy and strict access restrictions is also supported. Public exposure is disabled by default.

**Setup and shared hosting:** interactive terminal menu for initial origin setup, then optional REG.RU FastCGI setup with a dry run and an explicit `DEPLOY` confirmation. Managed relay deployment and rollback recycle stale FastCGI workers after an executable replacement; arbitrary manual `scp` is not covered.

## Compatibility

No intentional change to Noise/wire framing, origin API, `mbox://` provisioning format or HTTPS subscription schema. AUTO prefers QUIC, then HTTPS following a QUIC failure, with a 60-second QUIC retry cooldown. Transitions can interrupt active streams; QUIC DATAGRAM and seamless multipath are not implemented. HTTPS requires a working relay and QUIC requires valid TLS trust.

**Android and origin are not upgraded together automatically.** Known deployment state as of 2026-11-17: Admin UX7 is deployed on the VPS, but `mboxctl version` still reported beta.4.2. The origin update is a separate action with backup; changing the HTML version label does not replace a server binary.

## Known limitations

- REG.RU CPU monitoring is **not integrated**; the UI shows integration status, not live provider metrics.
- Android read-only diagnostics do not expose actual core packet/byte/stream counters yet.
- Independent physical-interface IPv4/IPv6/DNS/UDP leak captures, the complete Android Lockdown matrix and long-duration soak must be verified separately. No absolute anonymity, zero-leak, or universal DPI resistance is claimed.
- Public admin access requires HTTPS, controlled reverse proxy/Origin, separate authentication and restricted access; private SSH is the safer default.
- Existing QUIC TLS certificate/trust migration may still be needed. The cosmetic release does not resolve it.

## Evidence and artifacts

The maintainer reported **273 passing Python tests** and a successful Android release compile after UX8; the subsequent UX9 primary-button change also compiled. Before publication, re-run tests on the **exact final commit**, sign with the original production certificate, and perform an in-place `adb install -r` device test. Pre-final builds are not proof of final acceptance.

Expected artifact: `MBox-1.0.0-beta.4.3-android-arm64.apk` plus `.sha256`, with actual SHA-256 determined only after signing. No debug APK, user profiles, PSKs/HMAC secrets or signing key should be attached.
