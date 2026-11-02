# Stealth / camouflage

[English](../STEALTH.md)

MBox использует обычный HTTPS frontend на shared hosting, generic HTTP failures для invalid probes, случайные batch/padding/Noise chunk boundaries и jitter/backoff.

В текущей Android-ветке внешний HTTPS делает Chromium Cronet, а не Go TLS/HTTP. Это убирает очевидный Go implementation fingerprint и делает сетевой профиль более browser-like, но не гарантирует точную идентичность Chrome и не обещает проход любого DPI.

QUIC пока выключен, HTTP/2 включён. Go stack оставлен только для A/B/per-IP диагностики.

Текущая Cronet-ветка ещё не проходила полевой тест в БС; MegaFon неизвестен, `.ru` — тестовая гипотеза.
