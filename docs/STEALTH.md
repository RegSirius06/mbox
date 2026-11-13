# Traffic camouflage — MBox beta.4.1

[Русский](ru/STEALTH.md) · [Full limitations](TRAFFIC_CAMOUFLAGE_LIMITS.md) · [Fingerprint baseline](FINGERPRINT_BASELINE.md)

## Current architecture, not a browser impersonation guarantee

- **HTTPS:** Android delegates public HTTPS to bundled Chromium Cronet with HTTP/2/TCP; a configured REG.RU shared-hosting website/FastCGI can act as the public frontend. This removes the default Go TLS fingerprint on the public client→frontend hop, but a Cronet app is not guaranteed to look exactly like Chrome or Firefox.
- **QUIC:** direct native QUIC/HTTP3 over UDP to a configured VPS endpoint. The browser-like shared-hosting cover does **not** apply to this route.
- **Inside either transport:** Noise authenticates/encrypts MBox session data. HTTP path/header customizations, padding, randomized batch/chunk boundaries and polling timing can vary traffic patterns but do not guarantee invisibility to statistical DPI.

The frontend HTTPS terminator can observe outer HTTP metadata; an origin bound to a separate VPS remains a distinguishable backend hop. Noise secrets are not supplied to REG.RU FastCGI. Active probing, domain categorization, IP blocking, censorship policy and endpoint traffic correlation require measurements, not assertions. The project has no external independent camouflage audit.

**Unverified:** byte-for-byte browser TLS/JA4 and HTTP2 fingerprint equivalence, active-probe resistance, independent physical traffic capture, performance under restrictions, long soak. See [reproducible comparison](FINGERPRINT_BASELINE.md) and [limits](TRAFFIC_CAMOUFLAGE_LIMITS.md).
