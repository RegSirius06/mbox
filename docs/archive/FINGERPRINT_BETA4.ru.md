> Historical beta.4 source text; not the current release contract.

# Fingerprint и Cronet

<!-- MBOX-BETA4-CURRENT-DOCS -->
> **Версионный контекст:** ниже могут быть исторические наблюдения beta.2/beta.3. В **beta 4** работают прямой QUIC/HTTP3, UDP приложений и резервный HTTPS/Cronet. Актуальные сведения: [Android](ANDROID_BETA4.md), [транспорты](BETA_4_TRANSPORTS.md), [границы безопасности](SECURITY.md). Подписанный APK уже собран и установлен, но физический аудит утечек ещё не завершён.




[English](../FINGERPRINT_BASELINE.md)

В beta.2 внешний HTTPS делал Go. В текущей ветке:

```text
Go Noise/MUX/replay/ACK
  -> authenticated localhost bridge
  -> bundled Chromium Cronet
  -> DNS/TCP/TLS/ALPN/HTTP2
```

Это намного лучше, чем просто подменить User-Agent: наружный TLS/HTTP принадлежит одному Chromium-derived стеку. На реальном телефоне подтверждены Cronet + `h2` + Noise + tunnel.

Но MBox не заявляет точное совпадение с Chrome JA4/HTTP2 fingerprint. Нужен отдельный capture Chrome vs MBox Cronet vs MBox Go на одном телефоне/сети/endpoint. QUIC пока выключен.

Regression check:

```bash
python tools/mbox-fingerprint-check.py --require-h2 mbox-cronet.log
```

Для A/B:

```bash
sudo mboxctl user link USER --android-http-stack cronet --network-debug --deterministic-transport
```

Deterministic mode фиксирует camouflage timing/size choices, но не криптографическую случайность Noise.
