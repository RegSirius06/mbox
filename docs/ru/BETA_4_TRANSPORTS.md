> Историческое поведение beta.4; см. [beta.4.1](BETA4_1_STATUS.md).

# Транспорты beta 4: QUIC, HTTPS и UDP

[English](../BETA_4_TRANSPORTS.md) · [Android](ANDROID_BETA4.md)

| Режим | Внешняя сеть | Маршрут | UDP приложений |
|---|---|---|---|
| Auto | Предпочтителен QUIC, HTTPS — резерв | Переподключение/failover | Внутри зашифрованного MBox |
| QUIC | UDP (QUIC/HTTP3) | Прямой gateway VPS → origin | В HTTP/3 **streams**, не DATAGRAM |
| HTTPS | TCP (Cronet TLS/HTTP2) | Доступный frontend/FastCGI → origin | Внутри MBox поверх TCP, **не** внешний UDP |

Профиль QUIC содержит **обе** настройки: `transport.quic_url` и публичный X.509 `transport.quic_ca_pem`. Профиль только с HTTPS остаётся рабочим. В APK нет персональных адресов и сертификатов оператора. Auto — переключение с повторным подключением; активные потоки могут прерваться. После ошибки QUIC есть cooldown 60 секунд, но фоновый перебор транспортов при работающем HTTPS не обещается.

Штатный UDP в beta 4 включается через `enable_udp`, независимо от developer mode. Опасные debug-режимы должны оставаться закрытыми в release. Отсутствие утечек нельзя утверждать без физических packet-capture/Lockdown-тестов.
