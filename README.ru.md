# MBox

[English](README.md) · [Android](docs/ru/ANDROID_BETA4.md) · [Изменения](CHANGELOG.ru.md) · [Предварительный релиз](RELEASE_NOTES.ru.md)

**Актуальная версия: `1.0.0-beta.4` (Android ARM64, Android 10+).** MBox — экспериментальный VPN-транспорт с шифрованием Noise, UDP и выбором внешнего подключения. **Beta — не результат независимого аудита безопасности** и не гарантия работы в любой сети.

## Android: подключение за три шага

1. Скачайте `MBox-1.0.0-beta.4-android-arm64.apk` на [GitHub Releases](https://github.com/RegSirius06/mbox/releases), проверьте контрольную сумму и сертификат подписи из [release notes](RELEASE_NOTES.ru.md).
2. Запустите приложение и импортируйте личный профиль `mbox://` через **Settings → Paste profile link** или **Import from clipboard**. Ссылка содержит секреты: не публикуйте её и не присылайте в Issues.
3. Выберите **Auto** и нажмите **Connect**. Для QUIC в профиле должны быть заданы `transport.quic_url` и `transport.quic_ca_pem`; профиль только с HTTPS продолжит работать по HTTPS.

[Полное руководство Android](docs/ru/ANDROID_BETA4.md) · [Обновления и различие debug/release](android/README.ru.md)

## Как устроена связь

```text
                         +-- QUIC/HTTP3 через UDP --> QUIC gateway --+
Android VpnService ->    |                                              +--> MBox origin -> sing-box -> выход
Noise/MBox client -------+-- Cronet HTTPS/HTTP2 --> frontend/FastCGI -+
```

- **QUIC:** внешний UDP до VPS, внутри используется HTTP/3 **streams** (не QUIC DATAGRAM).
- **HTTPS:** TCP/TLS/HTTP2, возможно через REG.RU shared hosting и FastCGI. UDP приложений идёт **внутри** зашифрованных MBox-записей поверх TCP; это не прямой UDP наружу.
- **Auto:** сначала QUIC, при неудаче — HTTPS и переподключение; не multipath, не бесшовный перенос открытых соединений и не постоянное сравнение ping.

## Про безопасность

Приложение использует системный `VpnService`; для дополнительной защиты на телефоне рекомендуется включить **Always-on VPN** и **Block connections without VPN** (Lockdown). Сквозное отсутствие DNS/IPv4/IPv6-утечек при авариях и сменах сети **ещё не подтверждено независимыми packet-capture тестами**. Профили содержат долговременные данные доступа. У APK есть постоянная подпись, но подпись **не доказывает** отсутствие уязвимостей.

См.: [Модель безопасности](docs/ru/SECURITY.md), [проверки](docs/ru/TESTING.md), [известные ограничения](docs/ru/BETA4_RELEASE_STATUS.md). QUIC-сертификат тестового сервера истекает **7 декабря 2026 года**: администратору нужно заменить сертификат и обновить доверие в профилях заранее.

## Для администраторов и разработчиков

[Архитектура](docs/ru/ARCHITECTURE.md) · [Развёртывание](docs/ru/ADMIN.md) · [REG.RU FastCGI](docs/ru/REG_RU_SHARED_HOSTING.md) · [Транспорты](docs/ru/BETA_4_TRANSPORTS.md) · [Сборка Android release](docs/ru/ANDROID_RELEASE_ENGINEERING.md) · [Участие](CONTRIBUTING.ru.md)

Проблемы и результаты полевых проверок: [GitHub Issues](https://github.com/RegSirius06/mbox/issues). Не прикладывайте ключи, полный профиль, токены, конфигурации с секретами или необработанный сетевой дамп.
