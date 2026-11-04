# История изменений


## Не выпущено — кандидат beta.2.2

- Limit simultaneous origin requests and reject malformed HMAC headers before reading bodies / Ограничение параллельных HTTP-запросов origin и отказ до чтения body при плохих заголовках.
- Amortize origin nonce cache cleanup, retain bounded replay state / Периодическая очистка bounded nonce-кэша.
- Canonicalize UP/DOWN/CLOSE query parser, reject duplicated parameters / Защита от двусмысленных HTTP-параметров.
- Pin the release Android signing certificate; align beta.2.2 version / Пин сертификата APK и синхронизация версий.

Русская сводка текущей ветки. Полная историческая версия: [CHANGELOG.md](CHANGELOG.md).

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
