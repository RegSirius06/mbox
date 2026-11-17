# MBox beta.4.3 — staged release status (2026-11-17)

[Русский](ru/BETA4_3_STATUS.md) · [Overview](STATUS.md) · [Release notes](../RELEASE_NOTES.md)

- **Repository:** expected release identity `1.0.0-beta.4.3`, Android versionCode 10. No intended Noise/wire change.
- **Android source:** 273 Python tests reported passed after theme/brand UX8, release compilation reported passed after button-contrast UX9. The exact **final signed** artifact still needs rebuild, checksum verification and in-place device tests.
- **Admin:** UX7 deployed to origin VPS; local SSH access works. Optional HTTPS domain capability is installed but **not enabled** by default.
- **Origin:** last observed `mboxctl version` on the VPS was `1.0.0-beta.4.2`. Update separately with backup, downtime window, and explicit approval. Updating UI HTML is not a binary upgrade.
- **Shared hosting:** REG.RU FastCGI is a separate deployment. The installer offers an optional SSH wizard for first install only. No automatic upgrade of live REG.RU from Android/doc changes.
- **Limitations:** REG.RU CPU monitoring is not integrated, Android core packet/byte/stream counters are unavailable; full physical leak capture and long soak are not claimed.

**Prerelease, not RC1.** This document tracks stages. Do not treat it as proof that every production component has already been deployed or independently audited.
