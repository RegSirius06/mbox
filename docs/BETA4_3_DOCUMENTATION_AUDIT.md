# Documentation audit for beta.4.3 — 2026-11-17

[Русский](ru/BETA4_3_DOCUMENTATION_AUDIT.md) · [Docs index](README.md)

**Scope:** canonical project entry points, signed Android release instructions, server operator runbooks, status, and release body; EN and RU are separate files. Existing deep technical guides and historical beta notes are retained rather than rewritten without source-by-source behavioral verification.

## Reconciled current entry points

- `README.md` / `README.ru.md`: beginner-focused overview, dual QUIC/HTTPS ingress, no anonymity/no-leak guarantee.
- `docs/README.md` / `docs/ru/README.md`: current beta.4.3 navigation by audience.
- `docs/GETTING_STARTED.md` / `docs/ru/GETTING_STARTED.md`: Android user workflow.
- `docs/SERVER_QUICKSTART.md` / `docs/ru/SERVER_QUICKSTART.md`: first-time operator workflow and optional REG.RU relay.
- `android/README.md` / `android/README.ru.md`: client overview and build entry point.
- `docs/ANDROID_RELEASE_ENGINEERING.md` / `docs/ru/ANDROID_RELEASE_ENGINEERING.md` and `docs/RELEASE_ENGINEERING_BETA43.md` / `docs/ru/RELEASE_ENGINEERING_BETA43.md`: versionCode 10, original signer, exact APK gate, no accidental overwrite.
- `RELEASE_CHECKLIST*`, `RELEASE_NOTES*`, `docs/STATUS.md`, `docs/ru/STATUS.md`, beta.4.3 status, GitHub release draft: prerelease limits and actual deployed-state caveats.
- `CHANGELOG.md`, `CHANGELOG.ru.md`: new beta.4.3 entry prepended; previous history untouched.

## Historical and specialized documents intentionally retained

Version-specific pages (`ANDROID_BETA4_1`, `BETA4_1_STATUS`, `BETA4_2_STATUS`, beta.2/3/4 audit, test matrices), `docs/archive`, and dated field notes remain historical evidence; their old version numbers must not be replaced by search-and-replace. Detailed operator/security guides on QUIC TLS, REG.RU, subscriptions, Noise, routing and stealth stay authoritative for **their exact described components**, but are not evidence of completed final release acceptance. The index and current release notes point readers at up-to-date caveats.

## Not implemented / unverified

REG.RU provider CPU resource API polling; Android core byte/packet/stream counters; automatic fully verified renewal of the old QUIC trust setup; independent physical IPv4/IPv6/DNS/UDP no-leak capture; full Android Always-on Lockdown soak. Do not label these as completed.

## Deployment separation

Repository may be beta.4.3 while an origin VPS still reports beta.4.2. Replacing admin HTML does not upgrade `mboxctl`, origin or REG.RU FastCGI. Source/docs update is **not** a production operation. The final checklist includes verifying the deployed version separately.
