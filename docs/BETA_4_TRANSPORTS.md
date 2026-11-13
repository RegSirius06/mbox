> Historical beta.4 behavior; see [beta.4.1 status](BETA4_1_STATUS.md) for candidate changes.

# Beta 4 transport selection and UDP model

[Русский](ru/BETA_4_TRANSPORTS.md) · [Android](ANDROID_BETA4.md)

| Mode | Outer network | Path | Application UDP |
|---|---|---|---|
| Auto | QUIC preferred, HTTPS fallback | Reconnect/failover | Through encrypted MBox records |
| QUIC | UDP (QUIC/HTTP3) | Direct VPS gateway → origin | Encapsulated in HTTP/3 **streams**, not DATAGRAM |
| HTTPS | TCP (TLS/HTTP2 via Cronet) | Reachable frontend/FastCGI → origin | Encapsulated in MBox over TCP, **not** native external UDP |

QUIC is **per imported profile**: `transport.quic_url` and public X.509 `transport.quic_ca_pem` must both exist. HTTPS-only profiles remain supported. The APK has no embedded operator endpoints. Auto fallback is connection-level and can interrupt existing flows; a 60 s QUIC retry cooldown is used on failure, but no ongoing background racing of established HTTPS is promised.

UDP forwarding is a normal beta 4 capability through `enable_udp`, independent of developer mode. Unsafe debug options must remain disabled in release. Do not claim verified no-leak behavior until physical capture/Lockdown tests are complete.
