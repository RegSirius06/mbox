# Release notes — текущий development snapshot

[English](RELEASE_NOTES.md)

`1.0.0-beta.2.1` — совместимый с beta.2 hardening-релиз. Главное изменение — внешний Android HTTPS теперь делает Chromium Cronet, поэтому наружу не торчит характерный Go TLS/HTTP fingerprint. `v1.0.0-beta.2` остаётся предыдущим baseline. На реальном телефоне подтверждены Cronet+h2+Noise+tunnel, handover, airplane-mode recovery и session supersession.

Это не точная эмуляция Chrome и не гарантия обхода любого DPI. В активных БС текущая ветка ещё не тестировалась. MegaFon неизвестен; `.ru` — гипотеза для контролируемого сравнения с `.fun`.
