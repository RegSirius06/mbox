# Технический аудит 2026-11-02 — русская сводка

[English full audit](../TECHNICAL_AUDIT_2026-11-02.md)

Подтверждённые проблемы и исправления:

- single-IP Android selection -> multi-address/dual-stack diagnostic path + Cronet address racing;
- custom Go dialer мог выключать h2 -> исправлено и протестировано;
- unbounded response / redirects -> limits + deny;
- IPv4-only TUN -> IPv4+IPv6;
- pre-auth OPEN CPU gap -> rate/concurrency limiter;
- static key read per handshake -> load once;
- provisioning secret в query -> fragment;
- Go public fingerprint -> bundled Chromium Cronet;
- self-triggered VPN network callback -> `NOT_VPN` underlying monitor;
- blind retry без сети -> `WAIT_UNDERLYING`;
- stale sessions после метро -> authenticated `device_id` + server session supersession;
- stream limit 64 -> 128 после реального stress test и проверки cleanup.

Не подтверждены/не делались бессмысленные изменения:

- Noise flaw не найден, crypto protocol не переписывался;
- invalid OPEN уже был generic 404, banner не добавлялся;
- точная причина MegaFon не доказана.

Открыто: текущая ветка в БС, `.fun` vs `.ru`, Chrome-vs-Cronet fingerprint capture, signed provisioning/trust anchor, Always-on/lockdown, long soak/load, external audit.
