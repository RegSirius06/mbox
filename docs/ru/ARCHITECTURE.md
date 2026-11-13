# Архитектура MBox (beta.4.1)

<!-- MBOX-BETA4.1-ACTIVE-CONTEXT -->
> **Актуальный контекст beta.4.1:** клиент проверяет удалённый Noise heartbeat и восстанавливает QUIC без ручного выключения VPN. См. [статус](BETA4_1_STATUS.md), [пределы маскировки](TRAFFIC_CAMOUFLAGE_LIMITS.md) и [доказательства](TESTING.md). Старые наблюдения beta.2/3/4 — история, а не доказательство отсутствия утечек P0.

[English](../ARCHITECTURE.md) · [Транспорты](BETA_4_TRANSPORTS.md)

```text
Приложения Android -> VpnService/TUN -> HEV -> локальный MBox SOCKS -> Noise/MUX
                                                                |
                               +--> QUIC/HTTP3 через UDP -------+--> QUIC gateway VPS --> origin --> sing-box --> выход
                               |
                               +--> Cronet HTTPS/HTTP2/TCP --> shared hosting FastCGI --> origin
```

**Веб-фронтенд**, **origin** и **выход** могут находиться у разных провайдеров и в разных странах. REG.RU shared hosting обслуживает сайт и FastCGI relay, **но не является RU VPS**. Прямой QUIC обходит FastCGI. Последний участок может идти через другой сервер с sing-box.

**UDP приложений:** передаётся внутри аутентифицированных MBox-записей. На HTTPS внешняя сеть видит TCP; на QUIC внешний UDP, но внутри HTTP/3 используются streams. Это не QUIC DATAGRAM и не прямой UDP приложения мимо туннеля.

**Auto:** пробует QUIC при наличии его настроек, при ошибке переключается на HTTPS, при смене физической сети переоценивает транспорт. Активные соединения могут оборваться: это не multipath и не бесшовная миграция потоков.

**Профиль:** `mbox://` содержит секреты доступа. Пара `transport.quic_url` + `transport.quic_ca_pem` обязательна для QUIC и отсутствует у HTTPS-only профилей. В APK нет персонального адреса или сертификата VPS.

**Восстановление beta.4.1:** существующий аутентифицированный Noise framePing/pong, контроль gateway/HEV, состояния CONNECTING/VERIFYING/RECOVERING и переподключение с сохранением TUN. См. [статус](BETA4_1_STATUS.md).

**Защита и устойчивость:** Noise, DOWN replay/ACK, лимиты очередей, Android VpnService/Lockdown; всё это не заменяет независимый аудит, packet capture и проверку известных ограничений.
