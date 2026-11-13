# MBox status — 1.0.0-beta.4.1

[Русский](ru/STATUS.md) · [Full evidence](BETA4_1_STATUS.md) · [Release notes](../RELEASE_NOTES.md)

**Source candidate:** `1.0.0-beta.4.1`, Android `versionCode=8`, application ID `io.mbox.android`. Beta.4 wire and HTTPS-only profiles remain compatible. Native QUIC now has an authenticated session heartbeat and independent Android gateway/HEV watchdog; Auto remains reconnection-based.

**Maintainer-reported field result:** normal connection, screen-off idle, Wi-Fi/mobile handover, complete network loss and Auto passed on a beta.4.1 build. Client/server and native QUIC race tests passed, and a debug APK was built. Exact final production-signed APK acceptance and CI checks are still pending.

**Release blockers / warnings:** no independent physical-interface no-leak capture or full Android Lockdown matrix; old pinned-leaf QUIC TLS certificate expires December 7, 2026, and its CA migration/renewal has not been deployed. Universal DPI resistance, browser-identical fingerprint and audit claims are unsupported. Historical beta.4, beta.3 and beta.2 records remain historical.
