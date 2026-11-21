# MBox status — 1.0.0-beta.4.4 (pre-publication)

[Русский](ru/STATUS.md) · [Release notes](../RELEASE_NOTES.md) · [Checklist](../RELEASE_CHECKLIST.md)

**Local source/production testing:** Android `versionCode=11`; origin rekey/UDP modes and subscriptions/admin updated on an operator's VPS. Two allowed SOCKS5 exits DK/KZ verified by external country check; beta.4.3 client refreshed subscription and connected through default DK. Separate QUIC DATAGRAM on-demand attach, ~30s idle expiration and reattach after new UDP validated in Android/server logs. Screen-off, airplane mode and Wi-Fi/mobile handovers reported working. `install.sh --check`, Go/race and several focused tests passed; those results do **not** mean an end-to-end fresh-install/rollback or final GitHub release validation passed.

**GitHub:** repository main currently still has beta.4.3 until the operator commits/pushes beta.4.4. Production-signed final APK and GitHub prerelease publication must be verified separately. REG.RU is an independent FastCGI ingress and was not automatically upgraded.

**Unresolved:** REG.RU CPU/worker resource budget (high-priority operational risk); battery profiling, full Lockdown/physical IPv4/IPv6/DNS/UDP capture, long soak, independent crypto review, and notification test from published beta.4.3 → beta.4.4. No absolute DPI or leak guarantees.
