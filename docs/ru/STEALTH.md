# Stealth / camouflage

<!-- MBOX-BETA4-CURRENT-DOCS -->
> **Версионный контекст:** ниже могут быть исторические наблюдения beta.2/beta.3. В **beta 4** работают прямой QUIC/HTTP3, UDP приложений и резервный HTTPS/Cronet. Актуальные сведения: [Android](ANDROID_BETA4.md), [транспорты](BETA_4_TRANSPORTS.md), [границы безопасности](SECURITY.md). Подписанный APK уже собран и установлен, но физический аудит утечек ещё не завершён.




[English](../STEALTH.md)

MBox использует обычный HTTPS frontend на shared hosting, generic HTTP failures для invalid probes, случайные batch/padding/Noise chunk boundaries и jitter/backoff.

В текущей Android-ветке внешний HTTPS делает Chromium Cronet, а не Go TLS/HTTP. Это убирает очевидный Go implementation fingerprint и делает сетевой профиль более browser-like, но не гарантирует точную идентичность Chrome и не обещает проход любого DPI.

В прежней beta QUIC был выключен; в beta 4 есть отдельный QUIC/HTTP3 путь, а HTTPS использует HTTP/2. Go stack оставлен только для A/B/per-IP диагностики.

Текущая Cronet-ветка ещё не проходила полевой тест в БС; MegaFon неизвестен, `.ru` — тестовая гипотеза.
