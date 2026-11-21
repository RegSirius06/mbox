# Shared-host FastCGI CPU / worker exhaustion risk (REG.RU, beta.4.4)

**Severity:** high operational risk (P1); not yet a proven universal crash. **Component:** optional HTTPS ingress relay on REG.RU shared hosting.

## Problem
The relay can consume significant provider-metered CPU / process slots during HTTPS polling, concurrency, idle workers and reconnect storms. Shared-hosting limits are outside MBox control; exceeding them can throttle/kill FastCGI workers and disrupt users relying on HTTPS. The self-checked static binary does **not** prove acceptable CPU consumption. QUIC DATAGRAM bypasses this frontend and cannot fix the HTTPS path by itself.

## Reproduction / measurement plan
1. On an *authorized staging account*, record provider CPU/entry process limits, relay worker count and quota/FUP rules (redact account and domains).
2. Benchmark 0/1/2/4 concurrent clients, screen-off idle, HTTPS-only and QUIC→HTTPS failback; collect request rate, HTTP latency, worker RSS/CPU, error rates and provider throttling.
3. Compare old/new relay (same FastCGI backend), idle HTTP polling, worker recycle after deploy, and server request coalescing.
4. Verify loss/fallback and rollback under active throttling without modifying production unexpectedly.

## Expected
Bounded worker/resource consumption with defined safe concurrency, controlled idle/retry timers, honest provider-limit documentation and a reproducible mitigation. No silent unapproved shared-host changes.

**Do not attach:** HMAC keys, full subscription URLs, PSK, server.json, real user IPs, provider credentials or account IDs.
