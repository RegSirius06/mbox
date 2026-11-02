# Fingerprint и Cronet

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
