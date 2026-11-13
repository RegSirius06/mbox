> Historical beta.4 source text; not the current release contract.

# Traffic camouflage status and roadmap

<!-- MBOX-BETA4-CURRENT-DOCS -->
> **Version context:** Some details below describe earlier beta.2/beta.3 implementations or historical observations. In **beta 4**, native QUIC/HTTP3 and application UDP are supported, and HTTPS/Cronet is the fallback. See [current Android guide](ANDROID_BETA4.md), [transport model](BETA_4_TRANSPORTS.md), and [security limits](SECURITY.md). The signed APK has been built and installed, but comprehensive physical leak testing remains incomplete.




[Русская версия](ru/STEALTH.md)

The public frontend is a real HTTPS site on shared hosting. Invalid transport probes return ordinary HTTP failures rather than an MBox banner. API paths are inside TLS and are deployment flexibility, not a last-mile DPI bypass by themselves.

## Observable-flow diversification

MBox varies HTTP batch ceilings, padding length, Noise transport chunk boundaries and active polling/backoff timing. Exact retries reuse already-committed ciphertext, so diversification does not create nonce reuse.

Current generated defaults include:

- `padding_random_max_bytes = 16384`
- `batch_min_bytes = 73728`
- `noise_chunk_min_bytes = 16384`
- `noise_chunk_max_bytes = 61440`
- `active_min_ms = 120`
- `active_max_ms = 420`
- `active_backoff_max_ms = 2200`
- `active_jitter_percent = 35`

These controls reduce a stable size/timing signature; they do not make MBox indistinguishable from arbitrary browser traffic.

## Android public HTTPS fingerprint

The current Android branch delegates public HTTPS to bundled **Chromium Cronet**. The Go process retains Noise, MUX, replay/ACK and stream flow control; an authenticated loopback bridge carries only the HTTP transaction between Go and Cronet.

Real-device validation on 2026-11-02 confirmed Cronet was actually selected and negotiated HTTP/2 (`h2`) before a successful Noise tunnel. This removes the straightforward Go `crypto/tls` / `net/http` public implementation fingerprint used by released beta.2 and gives the outer connection a coherent Chromium-derived application networking persona.

Important limits:

- Cronet is not the Chrome browser UI/process model;
- MBox does not claim byte-for-byte Chrome ClientHello/JA4/HTTP2 settings;
- flow timing, destination history and application behavior can still be classifiers;
- QUIC is intentionally disabled for the first rollout;
- a controlled Chrome-vs-MBox capture remains required.

Use `--android-http-stack go` only for A/B/per-IP diagnostics, and `--deterministic-transport` only for controlled captures.

## Operator/white-list status

The current Cronet branch has not yet been field-tested during active white-list restrictions. The existing shared-hosting relay path and beta.2 wire semantics are preserved, so no regression is currently expected on previously working networks, but this is not a fresh field result. MegaFon remains unresolved; `.ru` vs `.fun` is a planned controlled experiment, not a guaranteed workaround.

## zapret2 lessons

MBox does not implement kernel/userspace TCP desynchronization tricks comparable to zapret2. Ordinary Android app sockets do not provide the same packet-level control. The current strategy is instead a normal Chromium-derived HTTPS stack plus transport-level size/timing diversification and a shared-hosting frontend.
