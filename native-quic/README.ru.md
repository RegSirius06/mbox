# MBox native QUIC / HTTP3 — beta.4.1

[English](README.md) · [Android](../docs/ru/ANDROID_BETA4_1.md) · [Ротация TLS](../docs/ru/QUIC_TLS_ROTATION.md)

Модуль native QUIC использует **HTTP/3 через UDP** для существующих аутентифицированных HTTP/Noise-записей MBox. На Android gateway даёт Go core приватный Unix HTTP socket и получает защищённые UDP-сокеты от Android broker. **QUIC DATAGRAM не используется:** UDP приложений вложен в надёжные HTTP/3 streams.

- `serve`: дополнительный QUIC listener на VPS, список разрешённых путей и подписанный forwarding к loopback origin. Для деплоя нужно отдельное разрешение администратора; ради heartbeat beta.4.1 обновлять gateway/origin не требуется.
- `client`: защищённый UDP к цифровому IPv4 endpoint из профиля; стандартная проверка TLS 1.3/X.509 цепочки, срока и IP SAN; никаких `InsecureSkipVerify`.
- Успешный QUIC handshake сам по себе **не доказывает** здоровье MBox-сессии. Go core проверяет аутентифицированный Noise ping/pong. Auto может перейти на Cronet HTTPS с обрывом существующих прикладных потоков.

```bash
cd native-quic
go vet ./...
go test -race -count=1 ./...
```

Инструкция оператора по CA/leaf, откату и миграции — [здесь](../docs/ru/QUIC_TLS_ROTATION.md). Профилям beta.4 с доверием к старому leaf может понадобиться однократный аутентифицированный реимпорт до истечения сертификата 7 декабря 2026. При выпуске клиентского обновления таймер ротации на VPS автоматически не активировать.
