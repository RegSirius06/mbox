# Testing status

[Русская версия](ru/TESTING.md)

## Noise beta.2 gates

The beta.2 security migration adds mandatory tests for:

- byte-for-byte reproduction of an independent `Noise_NKpsk0_25519_AESGCM_SHA256` vector (both handshake messages, handshake hash and transport ciphertext);
- server/client Noise OPEN interoperability;
- explicit rejection of legacy OPEN framing;
- expired policy rejection during Noise OPEN;
- multi-message logical Noise records;
- authenticated record-header tamper rejection;
- fuzzing of malformed Noise transport records;
- UP 504 retry using the same DOWN-request sequence URL and byte-identical encrypted request body;
- DOWN 502 retry using the exact same sequence URL.

The last two are important with stochastic shaping: randomness is allowed only before commit/seal. An ambiguous HTTP retry must never redraw padding/chunk boundaries or re-encrypt a committed record.


## Network-establishment regression tests

The P0 network suite additionally covers:

- first resolved IP unavailable, later IP succeeds;
- broken IPv6 with working IPv4 and the inverse;
- changed DNS answer order;
- all resolved addresses unavailable;
- HTTP/2 + ALPN `h2` with the custom pre-resolved dial path;
- redirect refusal;
- oversized response bodies and response headers;
- truncated response bodies;
- pre-auth handshake concurrency/rate limits;
- Noise transport counter exhaustion.

`tools/mbox-netprobe` is the non-credentialed layer probe used by the controlled carrier matrix in `docs/OPERATOR_TEST.md`.

## Automated correctness suite

The reliability regression suite covers:

- lost HTTP response after server DOWN commit and byte-identical encrypted replay;
- duplicate DOWN request sequence handling;
- strict concurrent GET/POST DOWN commit ordering;
- client receipt of DOWN responses out of order and duplicate replay suppression;
- bounded/chunked UP records and round-robin stream fairness;
- per-stream credit-window overrun rejection;
- a blocked/slow stream writer not blocking another stream;
- full-duplex stream traffic;
- UP retry using the same encrypted record/request sequence after ambiguous HTTP failure;
- authenticated session revocation/policy changes;
- per-client session caps, unlimited policy and expiry;
- parser fuzz targets for DOWN request metadata, MUX frames and HTTP envelopes.

Normal/race suite:

```bash
(cd server && GO111MODULE=off go test -race .)
(cd client && GO111MODULE=off go test -race .)
(cd relay  && GO111MODULE=off go test -race .)
python3 -m pytest -q tests
```

Fuzz smoke examples:

```bash
(cd server && GO111MODULE=off go test -run=^$ -fuzz=FuzzDownRequestMeta -fuzztime=30s .)
(cd client && GO111MODULE=off go test -run=^$ -fuzz=FuzzMuxFrameParser -fuzztime=30s .)
```

The opt-in long regression moves 3 GiB through the bounded UP scheduler under the race detector:

```bash
cd client
MBOX_LONG_TEST=1 GO111MODULE=off \
  go test -race -run 'TestMultiGBUploadChunking|TestFullDuplexQueueIsolation' -timeout 30m .
```

CI runs normal Go/Python tests, race jobs, fuzz smoke, Android build, secret scan, and the long transport job on manual/weekly execution with pinned tool versions.

## Field behavior already exercised

On 2026-11-02 the beta.2.1 APK was built/installed on a real ARM64 Android device and exercised on unrestricted connectivity:

- Cronet selected as the public stack;
- HTTP/2 negotiated with `alpn=h2`;
- MBox OPEN, Noise and tunnel established;
- Telegram, Instagram and video traffic worked;
- aggressive Telegram/Instagram/YouTube switching peaked at 64 concurrent streams and stream counts fell after idle;
- Wi-Fi/mobile underlying-network changes recovered;
- airplane mode produced `UNDERLYING_LOST -> WAIT_UNDERLYING -> UNDERLYING_AVAILABLE -> reconnect`;
- same-device stale sessions were immediately replaced by server `SESSION SUPERSEDE`;
- a transient DNS-not-ready race after network return recovered on the next underlying-change/retry.

These are lifecycle/connectivity checks, **not white-list qualification**.

## Production qualification still required

Passing synthetic correctness tests is necessary but not sufficient to call MBox production-qualified. Before broad deployment, record successful 24-hour and 7-day runs covering:

- repeated Wi-Fi ↔ LTE/default-network changes;
- packet loss, delay, TCP reset and connection stalls;
- frontend 502/504 after origin commit;
- frontend/origin/server restarts;
- deliberately slow/stalled SOCKS/sing-box next hop;
- 10, 50 and 100 concurrent clients while recording RSS, FDs, goroutines, queued bytes, open streams, error rates and latency;
- sustained low-resource (1 vCPU / 1 GiB) operation;
- long-running fuzz campaigns, not only smoke fuzzing.

A failed gate is a release blocker. The repository does not claim these multi-day/load gates have already passed.
