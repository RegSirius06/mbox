# Reliability and production-hardening status

This file maps the hardening review to concrete implementation/gates.

| Area | Current state |
|---|---|
| DOWN reliability | Request sequence + contiguous ACK + exact ciphertext replay cache; frames are popped only after replay registration. |
| DOWN ordering | One server DOWN commit mutex across GET/POST; one client delivery goroutine/reorder buffer. |
| UP buffering | Bounded per-stream queues, round-robin scheduler, `up_max_bytes` hard ceiling. |
| Flow control | Per-stream WINDOW credits, bounded writer queues and socket write deadlines. |
| Resource quotas | Global/per-client sessions, streams/session, total streams, stream-open/session-open rates, DOWN/replay/global queued-memory budgets, FD-limit check. |
| Revocation | Auth reload immediately kills removed/rotated authenticated sessions. |
| HTTP/upstream deadlines | Server read/write/header/idle deadlines; relay context/response-header deadline; upstream/local socket write deadlines. |
| CI | Pinned Go/JDK/Android/Gradle/AGP, Go tests, race, fuzz smoke, pytest, Android APK build, shell checks and secret scan. |
| Android provisioning | Explicit fingerprint confirmation, build-time Verified App Link host, App-Link QR support, transactional profile generations, persisted desired VPN state. |
| Operations | Unprivileged service user, systemd sandboxing, 64K NOFILE, config validation, live SOCKS health check, backup test-restore/integrity validation, safer lock/temp handling. |
| Soak/chaos/load | Harness/gates documented; 24h/7d production qualification still must be executed on deployment-like infrastructure. |
| External audit | Not completed. Required before calling MBox a security product. |
| Noise migration | Recommended as a versioned future protocol change; not implemented in this compatibility-breaking reliability pass. |

The project remains experimental until the soak/chaos/load gate and independent security review are complete.
