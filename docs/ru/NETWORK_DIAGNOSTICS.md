# Диагностика сети

<!-- MBOX-BETA4.1-ACTIVE-CONTEXT -->
> **Актуальный контекст beta.4.1:** клиент проверяет удалённый Noise heartbeat и восстанавливает QUIC без ручного выключения VPN. См. [статус](BETA4_1_STATUS.md), [пределы маскировки](TRAFFIC_CAMOUFLAGE_LIMITS.md) и [доказательства](TESTING.md). Старые наблюдения beta.2/3/4 — история, а не доказательство отсутствия утечек P0.

[English](../NETWORK_DIAGNOSTICS.md)

Главный принцип — локализовать стадию:

```text
DNS -> адрес -> TCP -> TLS -> сертификат -> ALPN -> HTTP -> MBOX_OPEN -> NOISE -> TUNNEL
```

В normal mode IP/session IDs редактируются/хэшируются. `--network-debug` включайте только временно: он раскрывает endpoint IP. PSK/private key/provisioning link логировать нельзя.

Android:

```bash
adb logcat -c
adb logcat -v threadtime -s MBox:I AndroidRuntime:E
```

Профиль:

```bash
sudo mboxctl user link USER --android-http-stack cronet --network-debug
```

Для per-IP контроля используйте `--android-http-stack go`.
