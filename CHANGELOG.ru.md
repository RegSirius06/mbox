## 1.0.0-beta.4.1 — кандидат prerelease

- Аутентифицированный Noise framePing/pong с таймаутом; VERIFYING/RECOVERING, контроль QUIC gateway/HEV, восстановление без обычного снятия TUN.
- Wire protocol и HTTPS-only профили beta.4 сохранены; native QUIC CI и TLS-тесты. Ротация TLS подготовлена, но не установлена на VPS.
- Разработчик сообщил о прохождении пяти сценариев подключения/восстановления; физические тесты утечек, приёмка signed APK и длительный soak ожидаются.

## 1.0.0-beta.4 — Android beta / 2026-11-13

- Прямой QUIC/HTTP3: внешний UDP до VPS gateway, MBox поверх HTTP/3 streams (не DATAGRAM).
- Auto предпочитает QUIC, при ошибке или смене сети переподключается по HTTPS/Cronet; не multipath и не бесшовная миграция потоков.
- UDP приложений внутри зашифрованного MBox как на HTTPS/TCP, так и на QUIC; штатный `enable_udp` не требует developer mode.
- Подписанный ARM64 APK с простым интерфейсом, QUIC-полями `transport.quic_url` + `transport.quic_ca_pem` в профиле и постоянной подписью.
- 110 Python-тестов, release APK собран и установлен. На debug подтверждены Wi-Fi QUIC и HTTPS на МТС/МегаФон; полная приёмка подписанного APK, Tele2, packet capture и длительный soak остаются открытыми.
- Тестовый сертификат QUIC истекает 07.12.2026, нужны ротация и обновление доверия пользователей.

См. [примечания к beta 4](RELEASE_NOTES.ru.md).

## Не опубликовано: 1.0.0-beta.4 (черновик)

- Реализована передача пользовательского UDP через MBox, включая UDP-over-HTTPS/TCP через REG.RU.
- Добавлен прямой native QUIC/HTTP3 gateway (HTTP/3 streams, не QUIC DATAGRAM).
- Android Auto: выбор транспорта и fallback QUIC → HTTPS при реконнекте.
- Выполнена изоляция QUIC через systemd DynamicUser/NoNewPrivileges; соединение после перезапуска проверено.
- В полевых тестах подтверждены HTTPS, QUIC, смена Wi-Fi/LTE; на одном устройстве заработал МегаФон.
- **До публикации:** release APK должен содержать QUIC, подписываться постоянным ключом и проходить установочные проверки. Причина сбоя FastCGI 12.11.2026 неизвестна.

## 1.0.0-beta.3 — 2026-11-05 (предварительный релиз)

- **Несовместимость с публичной beta.2.1:** при обязательном `cmac` старый клиент не сможет выполнять HTTP UP/DOWN/CLOSE. Noise и PSK/профили не менялись; beta.2.2 с `cmac` совместима.
- Аутентификация управляющих HTTP-параметров, защита ACK/CLOSE от подделки посредником.
- Инкрементальное обновление credentials без копирования всей таблицы.
- Защита origin от перегрузки, ранняя проверка HMAC-заголовков, ограниченный nonce cache и строгий разбор параметров `seq/ack/max/cmac`.
- Скрипт контролируемого обновления root-managed VPS, требование pinned release APK signer, Android `versionCode=6` и согласованный CLI.
- Полевое наблюдение 05.11.2026: на одном Android новая версия работала через REG.RU во время активных мобильных белых списков. Отсутствие утечек packet capture и 24/7 нагрузка не проверены.

Архив предыдущих версий: [CHANGELOG.md](CHANGELOG.md).

## 1.0.0-beta.2.1 — 2026-11-02

- Chromium Cronet для внешнего HTTPS Android;
- HTTP/2, QUIC пока выключен;
- stage diagnostics и `mbox-netprobe`;
- dual-stack/handover/airplane-mode wait;
- stable `device_id` + same-device session supersession;
- pre-auth OPEN limits;
- provisioning fragment вместо query;
- modularization client/server;
- `max_streams_per_session=128` после реального stress test.

Текущая ветка ещё не проверена при активных БС. MegaFon неизвестен. `.ru` против `.fun` — следующий контролируемый тест.
