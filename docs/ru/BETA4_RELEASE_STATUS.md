# MBox beta 4 — подтверждённое состояние релизного кандидата

[English](../BETA4_RELEASE_STATUS.md) · [Release notes](../../RELEASE_NOTES.ru.md)

**Версия исходников:** `1.0.0-beta.4`. Подписанный APK ARM64 собран, проверен и установлен на Android. **Это ещё не полная независимая приёмка всех транспортов именно подписанной сборки и не гарантия безопасности.** Хеш APK и отпечаток сертификата — в [release notes](../../RELEASE_NOTES.ru.md). GitHub-публикация — отдельный шаг после локальной сборки.

Актуальная схема: Android `VpnService` → Noise/MBox → прямой QUIC/HTTP3 streams (внешний UDP до VPS) или Cronet HTTPS/HTTP2/TCP через FastCGI. Auto предпочитает QUIC, при ошибке переподключается по HTTPS. QUIC адрес и X.509 trust — **в профиле**, не в APK. UDP внутри HTTPS остаётся поверх TCP; нет multipath и QUIC DATAGRAM.

На debug реально проверены Wi-Fi QUIC, МТС/МегаФон HTTPS, Auto/переподключения, Telegram. Автоматические Python-тесты: 110 passed; сборка/подпись/установка release подтверждены. Tele2 на второй трубке, физический packet capture, длительный soak и независимый аудит остаются открытыми. Тестовый QUIC-сертификат истекает 07.12.2026.
