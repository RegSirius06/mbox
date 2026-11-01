# Traffic camouflage status and roadmap

The public frontend is a real HTTPS site on shared hosting. MBox transport paths coexist with an ordinary decoy website; invalid transport probes return ordinary HTTP failures instead of a protocol banner.

`deploy/regru/site/index.html` and `404.html` are packaged examples only. Administrators may replace them with any site and may configure their own `open_path`, `up_path`, `down_path` and `close_path`. The API path names are deployment flexibility, not the primary last-mile anti-DPI mechanism because an external ISP cannot read paths inside HTTPS.

## beta.2 observable-flow diversification

beta.2 deliberately changes characteristics an external observer can infer through encrypted traffic:

- HTTP upload/download batch ceilings vary between a configurable minimum and maximum;
- padding uses a random byte range instead of always landing on powers-of-two classes by default;
- a logical HTTP MBox record is split into randomly sized Noise transport messages (within Noise's 65535-byte message limit);
- active polling starts in a configurable random range and uses stochastic backoff/jitter rather than one fixed cadence;
- exact retries reuse the already-built ciphertext so diversification never violates nonce/replay correctness.

These controls reduce a stable size/timing signature. They do not claim to make MBox indistinguishable from a browser.

Default knobs are generated in the `camouflage` object/config section:

- `padding_random_max_bytes = 16384`
- `batch_min_bytes = 73728`
- `noise_chunk_min_bytes = 16384`
- `noise_chunk_max_bytes = 61440`
- `active_min_ms = 120`
- `active_max_ms = 420`
- `active_backoff_max_ms = 2200`
- `active_jitter_percent = 35`

Keep client and server camouflage limits compatible. Extreme randomness is not automatically stealthier; the objective is a broad, non-deterministic flow shape without introducing a unique pathological pattern.

## Remaining limitation: native Go TLS fingerprint

The MBox Go client still uses Go's native TLS ClientHello and HTTP implementation. beta.2 does **not** claim Chrome/Firefox browser fingerprint impersonation.

Copying only an uTLS ClientHello is not enough to claim browser indistinguishability. A browser identity is correlated across TLS extensions/key shares, ALPN, HTTP/2 settings/framing, connection pooling/resumption, packet sizes and timing.

The next major stealth/client task should therefore evaluate a real browser-derived Android networking stack (for example Chromium/Cronet/HttpEngine) or another coherent transport persona rather than merely switching ClientHello strings.

## zapret2 lessons

`zapret2` demonstrates a different anti-DPI family: packet-level desynchronization using fake protocol payloads, TCP segmentation/disorder and sequence overlap. Those mechanisms operate below a normal application HTTP client (NFQUEUE/WinDivert/raw packet processing), so MBox does not copy them directly on Android in beta.2.

The transferable design ideas are: avoid one immutable strategy, vary split points/flow shape, measure against real DPI behavior, and keep the low-level anti-DPI layer separable from cryptographic correctness. A router/Linux deployment may additionally use zapret2 outside MBox if appropriate.
