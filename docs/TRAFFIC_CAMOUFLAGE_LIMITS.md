# MBox beta.4.1 — traffic camouflage: observed boundaries

[Русский](ru/TRAFFIC_CAMOUFLAGE_LIMITS.md) · [Fingerprint tests](FINGERPRINT_BASELINE.md) · [Security](SECURITY.md)

## What an observer can see

MBox protects application data within authenticated Noise records. This does **not** hide the fact that an outer connection exists, its destination IP/domain where visible, protocol family, timing, sizes, burst patterns, total bytes or communication frequency.

| Path | Outer appearance | Not hidden or guaranteed |
|---|---|---|
| QUIC direct | TLS-protected QUIC/HTTP3 over UDP to the configured VPS | Direct VPS destination and UDP/QUIC traffic; no REG.RU cover on this path |
| HTTPS/Cronet | Chromium-derived HTTPS/HTTP2 over TCP to the configured frontend, potentially REG.RU shared hosting | Cronet is not exactly Chrome; TLS/ALPN/HTTP2 behavior and flow statistics require measurement |
| REG.RU FastCGI relay | Ordinary shared-hosting website and configured HTTPS forwarding paths | Public cover site's existence does not make automated polling or the provider-to-origin hop invisible |

A passive observer cannot read authenticated Noise application data without keys; traffic-analysis correlation is a separate question. On the shared-hosting server, the HTTPS termination / relay may observe HTTP paths and timing; the upstream origin is also a separate network connection and needs its own trust and access controls. Noise keys are not intentionally given to the relay.

## Controls that already exist

- Per-profile cover paths and configurable HTTP headers, bounded Noise records and padding; variable batch ceilings, chunk boundaries and polling intervals.
- Android HTTPS uses bundled Cronet; debug-only network instrumentation is not intended for the release APK.
- QUIC gateway uses a restricted route allowlist and authenticated origin forwarding; the application traffic remains protected by Noise.

## What has NOT been demonstrated

- Byte-identical Chrome, Firefox or any other browser fingerprint, including HTTP/2 SETTINGS and JA4 across app versions.
- Resistance to active probing, protocol-specific blocking, statistical DPI or network-level traffic correlation on all operators.
- Complete leak safety across service death, Doze, IPv6, DNS, captive portals, network switches and all Android vendor ROMs.
- Provider permission to operate a proxy on shared hosting: check current REG.RU terms independently.

## Required reproducible test

Use the **same physical device, endpoint and network** to compare a browser and MBox HTTPS/QUIC. Capture external TLS ClientHello, SNI/ALPN where visible, QUIC version, five-tuples, connection counts, session resumption, request intervals and sizes; measure with an instrumented test endpoint for encrypted HTTP/2 settings. Also capture frontend→origin traffic under authorized access. Run on the final production-signed APK. Avoid publishing private endpoints, provisioning links, decrypted HTTP paths or secrets. Record date, Android build, provider, mode and a sanitized evidence summary. Until then describe the behavior as *browser-derived*, not *browser-identical*.
