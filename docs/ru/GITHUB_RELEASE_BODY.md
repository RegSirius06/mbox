# MBox v1.0.0-beta.4.3 — предварительный выпуск Android и веб-панели

**ЧЕРНОВИК. НЕ ПУБЛИКОВАТЬ, ПОКА ЕСТЬ PENDING.**

Android 10+ / ARM64 · `io.mbox.android` · `versionCode=10` · **Pre-release, не RC1**.

**Новое:** Android-интерфейс, русский/английский, системная светлая/тёмная тема и контрастные Connect/Disconnect, фирменная иконка MBox, управление профилями и подписками, отключение из уведомления, скрытая диагностика, оповещения GitHub; обновлённая веб-панель с необязательным HTTPS-доменом; интерактивная первичная установка shared hosting.

**Подключение:** QUIC предпочтителен, при недоступности используется HTTPS-вход (например, FastCGI на REG.RU). Шифрование Noise/wire и формат `mbox://` намеренно не изменялись; старые профили beta.4.2 должны оставаться совместимыми. Смена транспорта может обрывать сессии. QUIC DATAGRAM/multipath нет.

**Ограничения:** не пройден независимый физический аудит отсутствия утечек, не гарантирован обход любого DPI; CPU-метрики REG.RU не подключены, счётчики пакетов/байтов/потоков Android пока недоступны. Версия origin на VPS может отличаться до его отдельного обновления. Публичная панель требует TLS и ограничений доступа.

**Артефакты:** только production-signed APK. Не публиковать debug/unsigned APK, профили, ссылки подписок, keystore или секретные runtime-файлы.

- APK: `MBox-1.0.0-beta.4.3-android-arm64.apk`
- SHA-256 APK: `PENDING_SIGNED_APK_SHA256`
- SHA-256 сертификата подписи: `17FD9F930B8356BECA8251DD215220771A3D020E56FB6A703B526C35CCCD7586`
- Git commit: `PENDING_FINAL_GIT_SHA`
- Финальные испытания APK и статус VPS: `PENDING_FINAL_VERIFICATION`

[Русские release notes](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4.3/RELEASE_NOTES.ru.md) · [English release notes](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4.3/RELEASE_NOTES.md)
