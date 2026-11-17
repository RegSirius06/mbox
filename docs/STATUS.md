# MBox status — 1.0.0-beta.4.3

[Русский](ru/STATUS.md) · [Release notes](../RELEASE_NOTES.md) · [Release process](RELEASE_ENGINEERING_BETA43.md)

**Source candidate:** 1.0.0-beta.4.3; Android `versionCode=10`, package `io.mbox.android`. Reported latest checks: 273 Python tests passed after UX8, Android release compilation passed again after UX9. The exact final production-signed APK needs its own acceptance evidence, checksum, device upgrade, and CI run before publication.

**Deployed VPS:** Admin UX7 works, but `mboxctl version` was reported as beta.4.2. Origin update is a separate operation with a maintenance window and backup. The shared-host FastCGI frontend is also a separate deployment. Do not call the whole production setup upgraded until these components are checked.

**Open security evidence:** physical IPv4/IPv6/DNS/UDP capture, Android Lockdown matrix, long soak, and complete TLS certificate rotation remain separate from a cosmetic prerelease. No independent security audit or universal DPI bypass claimed. REG.RU CPU metrics and Android core packets/bytes/streams counters are not integrated.
