# MBox v1.0.0-beta.4.4 — предварительный релиз

**ЧЕРНОВИК — ПЕРЕД ПУБЛИКАЦИЕЙ ЗАПОЛНИТЬ ВСЕ PENDING.** Android 10+, ARM64, `io.mbox.android`, versionCode 11. **Pre-release, не RC1.**

## Изменения

- QUIC DATAGRAM для UDP приложений по требованию: засыпает при простое, безопасно присоединяется повторно с отдельным AEAD-ключом каждой эпохи; без постоянных DATAGRAM PING. TCP/управление — надёжные streams.
- Скрытые режимы UDP: AUTO (по умолчанию), STREAMS only, DATAGRAM only. Оптимизации восстановления/батареи и оформление выбора профиля Android.
- Одна подписка с v2-профилями стран, аутентифицированным `egress_id`; beta.4.3 получает v1 и default. Разработчик реально проверил DK/KZ.
- Панель MBox, безопасность установщика, закрытый SSH-доступ, самопроверка сборки FastCGI и откат origin. QUIC и HTTPS — разные входы.

## Ограничения / Issues

**CPU/воркеры FastCGI REG.RU не исправлены**: DATAGRAM не сокращает CPU нагрузки HTTPS-фронтенда. Батарею нужно измерить, нет независимого аудита утечек/Lockdown и криптографии, гарантий обхода DPI нет. Уведомление о релизе нужно проверить на beta.4.3 после публикации.

**Артефакты:** только production-signed APK, без debug/профилей/keystore/runtime-секретов.
- APK: `MBox-1.0.0-beta.4.4-android-arm64.apk`
- SHA-256 APK: `PENDING_SIGNED_APK_SHA256`
- SHA-256 сертификата: `17FD9F930B8356BECA8251DD215220771A3D020E56FB6A703B526C35CCCD7586`
- Git SHA: `PENDING_FINAL_GIT_SHA`
- Испытания подписанного APK / статус VPS: `PENDING_FINAL_VERIFICATION`
