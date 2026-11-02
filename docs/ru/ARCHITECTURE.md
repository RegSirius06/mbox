# Архитектура

[English](../ARCHITECTURE.md)

```text
Android apps
 -> dual-stack VpnService/TUN
 -> HEV
 -> MBox SOCKS 127.0.0.1:10890
 -> Go Noise/MUX
 -> authenticated localhost bridge
 -> Chromium Cronet HTTPS
 -> REG.RU shared hosting/FastCGI
 -> MBox server 127.0.0.1:18449
 -> sing-box mixed 127.0.0.1:2080
 -> direct/chain exit
```

Shared hosting, origin и final exit — независимые слои.

DOWN надёжен через request sequence + contiguous ACK + exact encrypted replay. UP bounded/chunked, per-stream WINDOW/queues/backpressure.

Android monitor смотрит только `INTERNET + NOT_VPN`. При hard loss ждёт сеть. `device_id` внутри Noise OPEN позволяет server supersession stale-сессии того же телефона.
