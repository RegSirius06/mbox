# MBox v1.0.0-beta.4.3 release checklist

[Русский](RELEASE_CHECKLIST.ru.md) · [Full release commands](docs/RELEASE_ENGINEERING_BETA43.md)

**Prerelease, not RC1 or an independent security certification.** Check each item against the **exact final commit**.

## Source and version alignment

- [ ] `VERSION=1.0.0-beta.4.3`, Gradle `versionName=1.0.0-beta.4.3`, `versionCode=10`, `io.mbox.android`; CLI version and APK gate match.
- [ ] No unplanned Noise/wire, origin API, or provisioning/subscription schema changes; beta.4.2 profile compatibility tested.
- [ ] `git status`, `git diff --check`, code/secret review: no compiled APK, profiles, keystore or runtime DB committed.
- [ ] All Python tests, Go/race for client/server/relay, native QUIC vet/race, and Android release build pass at the final commit.
- [ ] `python3 tools/check-release-docs.py` passes; current RU/EN documentation agrees while history is retained.

## Android production release

- [ ] Original production PKCS12 keystore (outside Git) and trusted SHA-256 signer fingerprint verified.
- [ ] An existing beta.4.3 APK was backed up; new output was **signed and checked**, not merely built by Gradle.
- [ ] `tools/check-production-apk.sh` validates signature, manifest, native QUIC/core and absence of debug assets; `.sha256` matches.
- [ ] `adb install -r` from previously signed beta preserves profiles/subscriptions.
- [ ] Real-device system light/dark theme (including Connect/Disconnect), RU/EN, launcher icon, seven-tap diagnostics and notification Disconnect tested.
- [ ] QUIC, HTTPS, AUTO fallback, network handover/loss, idle, recovery and long-running behavior tested.

## Server and operations

- [ ] Documented source/Android version and **deployed origin version** accurately; do not equate UI label with installed CLI.
- [ ] If an origin upgrade is planned, execute separately with backup, rollback plan and a maintenance window; verify VPS `mboxctl version`.
- [ ] Admin SSH mode checked; HTTPS public domain remains opt-in; no unapproved REG.RU shared-host updates.
- [ ] Open checks disclosed: physical IPv4/IPv6/DNS/UDP packet captures, Android Lockdown, certificate rotation and long soak.

## Publication

- [ ] README, Release Notes, changelog, release body, and documentation navigation updated separately in RU/EN.
- [ ] GitHub CI green on final SHA; tag `v1.0.0-beta.4.3` points to that commit.
- [ ] GitHub Release explicitly marked **Pre-release**; attach production-signed ARM64 APK plus `.sha256` only.
- [ ] Release body includes actual APK checksum and commit SHA; no `PENDING`, sensitive URLs or secrets.
