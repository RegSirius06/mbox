# Диагностика сети

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
