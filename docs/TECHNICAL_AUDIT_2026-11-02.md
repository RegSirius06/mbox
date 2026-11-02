# MBox technical audit — 1.0.0-beta.2.1 hardening branch

[Русская версия](ru/TECHNICAL_AUDIT_2026-11-02.md)

Date: 2026-11-02

## Scope and audit rule

The audit was performed against the retained MBox 1.0.0-beta.2 source tree and Android wrapper, server, relay, administration tooling, deployment files and tests. Findings below distinguish confirmed code defects from hypotheses that still require carrier/device evidence.

No claim is made that MegaFon is blocking a specific TLD, TLS fingerprint or MBox protocol until a field capture identifies the failing stage. The patch first adds stage-oriented diagnostics, then fixes confirmed resilience/security defects. The same patch now also contains an Android Cronet outer-HTTPS implementation so Go-vs-Cronet A/B testing is possible without changing Noise/MUX/reliability.

## Findings

| Severity | Component | Problem | Evidence | Implemented / proposed fix |
|---|---|---|---|---|
| HIGH | Android DNS / client dial | Android resolved multiple addresses but selected one endpoint IP, preferring IPv4 | beta.2 `getAllByName()` path collapsed the answer set to one `MBOX_CONNECT_IP` | **Fixed:** retain bounded A/AAAA set; Go diagnostic stack uses staggered multi-address fallback; hostname remains SNI/certificate identity |
| HIGH | Android VPN | TUN covered IPv4 only | beta.2 builder had IPv4 address and `0.0.0.0/0` only | **Fixed:** add IPv6 TUN address and `::/0`; HEV config is dual-stack |
| HIGH | Provisioning confidentiality | HTTPS App Link put secret-bearing profile in query | `?profile=mbox://...` can reach web logs/history/analytics | **Fixed compatibly:** new links use URL fragment; Android still imports legacy query form |
| HIGH | Provisioning authenticity | Compromised frontend can replace profile/server identity | no trust anchor external to the delivered profile | **OPEN:** needs a real signing/trust model; adding Ed25519 with the verification key in the same untrusted profile would be meaningless |
| MEDIUM | HTTP/2 | Custom Go `DialContext` prevented normal automatic HTTP/2 attempt behavior | custom `http.Transport`, no `ForceAttemptHTTP2` | **Fixed:** `ForceAttemptHTTP2=true`; h2/ALPN regression test |
| MEDIUM | HTTP resources | Response body was unbounded; response headers had no explicit tight cap | `io.ReadAll(resp.Body)` | **Fixed:** 8 MiB response-body cap and 64 KiB header cap; oversize/truncation tests |
| MEDIUM | Redirects | MBox transport followed standard Go redirects | no `CheckRedirect` override | **Fixed:** redirects denied; test covers cross-origin redirect |
| MEDIUM | Diagnostics | Failure could not be located at DNS/TCP/TLS/HTTP/OPEN/Noise stage | beta.2 logs lacked stage chain, selected address, ALPN/HTTP version | **Fixed:** structured reason codes and NORMAL / DEBUG NETWORK modes |
| MEDIUM | Android handover / retry | Wi-Fi/mobile transition relied on eventual socket failure; reconnect had weak backoff behavior | no default-network callback; backoff reset too early | **Fixed:** network callback restarts outer transport/core, re-resolves DNS; 1–30 s exponential backoff with jitter, stable-run reset |
| MEDIUM | OPEN DoS | Existing session-open limit happened after Noise auth work | `registerSession()` enforced limits after successful handshake | **Fixed:** pre-auth concurrent-handshake and attempts/s limiter; generic camouflage denial; load test |
| MEDIUM | Server key handling | responder static private key was loaded from disk per OPEN | handshake path read key repeatedly | **Fixed:** load once at startup; permissions reject group-write/others while preserving intended root:mbox group-read deployment |
| MEDIUM | Android public fingerprint | beta.2 exposed Go TLS/HTTP implementation behavior | outer transport was Go `crypto/tls` + `net/http`; Go UA marker possible | **Fixed in audit branch:** default external HTTPS runs through bundled Chromium Cronet; Go remains A/B/diagnostic mode |
| MEDIUM | Fingerprint consistency | changing only UA/ClientHello would create a mixed persona | beta.2 application behavior and Go TLS/HTTP are correlated | **Avoided:** no fake Chrome UA; Cronet owns public TLS/ALPN/HTTP2/pooling/resumption and transport-level headers |
| MEDIUM | Android lockdown | per-app exclusion used to prevent tunnel recursion is incompatible with lockdown | package excluded from own VPN | **Handled conservatively:** advertise no Always-on support yet; true lockdown needs socket-specific `VpnService.protect()` architecture |
| MEDIUM | Supply chain | Gradle distribution checksum absent; binary/dependency provenance needed stronger documentation | beta.2 wrapper config/dependencies | **Partial:** Gradle distribution checksum and HEV hash checks; immutable Actions SHA / wrapper-JAR provenance remain P2 |
| LOW | Local interfaces | Android local SOCKS is loopback by default; generic manual config can bind elsewhere | generated config is `127.0.0.1` | default safe; non-loopback manual deployments remain administrator responsibility |
| INFO | Noise | No concrete flaw justifying protocol redesign was confirmed | NKpsk0/vector/replay/order/race tests pass | **Kept:** no primitive/pattern redesign; only fail-closed counter-exhaustion guards/tests |
| INFO | Active probing | invalid OPEN already avoids unique version/banner disclosure | generic camouflage behavior | no needless protocol rewrite |
| INFO | systemd | service was already substantially hardened | unprivileged user, NoNewPrivileges, ProtectSystem, ProtectHome, PrivateTmp/Devices, capabilities/memory controls | retained |

No CRITICAL issue was confirmed in this pass.

## MegaFon: what the code can and cannot prove now

Observed field state supplied for 2026-11-02:

- ordinary Wi-Fi is an unrestricted baseline;
- MTS works under the observed restrictions;
- T2 works under the observed restrictions;
- MegaFon cannot open the same `.fun` hostname even in ordinary Chrome.

That observation strongly suggests the specific MegaFon failure can occur before MBox/Noise, but by itself it does **not** distinguish DNS manipulation, IP/subnet policy, hostname/SNI/category filtering, TLS filtering, IPv4/IPv6 path differences or another operator mechanism.

The patch adds:

1. MBox stage diagnostics: `DNS -> TCP -> TLS -> HTTP -> MBOX_OPEN -> NOISE -> TUNNEL`.
2. `tools/mbox-netprobe` to test DNS, every returned IP, TCP, TLS with original SNI, ALPN and HTTP without MBox credentials.
3. Android `cronet` / `go` A/B transport selection.
4. `--deterministic-transport` for reducing camouflage timing/size variability during controlled capture. Noise/crypto randomness is not disabled.
5. `docs/OPERATOR_TEST.md` with the `.fun` / `.ru` matrix.

Target conclusions after field testing should be concrete, for example:

- `.fun` and `.ru` resolve to the same IP; TCP succeeds for both; `.fun` TLS stalls before ServerHello while `.ru` succeeds -> SNI/hostname/TLS policy strongly implicated;
- both names fail TCP to the same IP -> IP/subnet/operator routing/allowlist issue;
- Chrome and Cronet-MBox succeed but Go-MBox fails -> Go TLS/HTTP/address behavior is implicated;
- Chrome succeeds but Cronet-MBox and Go-MBox reach HTTP then fail OPEN/Noise -> problem is inside MBox/auth/protocol, not carrier reachability.

## Fingerprint implementation in the same patch

### Architecture

Android default path in the audit branch:

```text
MBox Go core (Noise/MUX/replay/ACK)
        |
        | authenticated binary protocol on 127.0.0.1
        v
Android CronetBridge
        |
        v
Chromium Cronet -> DNS/TCP/TLS/ALPN/HTTP/2 -> public endpoint
```

The loopback bridge carries only the already-encrypted MBox HTTP record plus HTTP method/path/application headers/status/body. The 32-byte per-process bridge token is random and is passed only through the local process environment. The bridge does not move PSK/server private keys/Noise state into Java.

Cronet mode deliberately strips Go-owned transport headers such as `User-Agent`, `Accept-Encoding`, `Host`, `Content-Length`, `Connection`, `Transfer-Encoding` and `Upgrade`; Cronet owns those details. MBox application headers such as `Accept` and `Content-Type` remain available.

NORMAL Cronet mode does not pre-resolve the endpoint in the Android wrapper, avoiding a duplicate DNS lookup before Cronet performs its own resolution/address racing. `--network-debug` deliberately enables an explicit A/AAAA pre-resolution for troubleshooting; `go` mode always needs the resolved candidate set for explicit fallback.

The first rollout sets HTTP/2 enabled and QUIC disabled. This changes one variable at a time: TCP/TLS/h2 behavior is moved to Chromium-derived networking without introducing a UDP/QUIC dependency into the carrier-resilience experiment.

### A/B modes

Provisioning supports:

```text
--android-http-stack cronet   # default fingerprint mode
--android-http-stack go       # diagnostic control with explicit per-IP attempts
--deterministic-transport     # stable camouflage timing/size choices for captures
```

The Go mode is retained because it exposes exact candidate IP attempt logs. Cronet performs its own resolver/address racing internally; Android pre-resolution still logs A/AAAA and Cronet `RequestFinishedInfo` provides DNS/connect/SSL/TTFB/total/reuse/protocol metrics.

### What this does *not* claim

Cronet is a coherent Chromium-derived application networking stack, but this patch does not claim exact Chrome browser impersonation. A browser fingerprint also includes browser-version-specific ClientHello details, HTTP/2 settings, resumption history, application request behavior, packet sizes and timing. Those properties must be compared empirically on the same Android device/network/endpoint.

The patch includes `tools/mbox-fingerprint-check.py` to catch regressions such as accidentally falling back to public Go networking, losing h2 on an h2-capable endpoint, or leaking `Go-http-client` markers into the diagnostic log.

## Major changes by area

### P0-A diagnostics

Files include `client/network.go`, `client/main.go`, Android VPN wrapper/profile code, `mboxctl`, `docs/NETWORK_DIAGNOSTICS.md`.

Reason codes cover DNS, TCP timeout/refused/unreachable, TLS/certificate, HTTP status/body/protocol, Android bridge/Cronet errors, MBox OPEN and Noise auth. Normal mode hashes IPs/session IDs; debug mode may reveal endpoint IPs but not PSKs/private keys/provisioning payloads.

### P0-B multi-IP / dual stack

Android retains up to 16 unique A/AAAA answers. Go mode interleaves families while respecting the resolver's first-family preference, staggers attempts by 250 ms and keeps the original hostname for SNI/certificate validation. Android TUN routes both IPv4 and IPv6.

Tests cover first-address failure, broken-v6/working-v4, broken-v4/working-v6, all dead, duplicate/mixed answers and answer-order changes.

### P0-C HTTP/2 / ALPN

Go diagnostic transport uses `ForceAttemptHTTP2=true`. An h2-capable test server requires negotiated `h2` / `HTTP/2`.

Cronet mode logs `UrlResponseInfo.getNegotiatedProtocol()` and request-finished metrics. On an h2-capable production endpoint the regression tool can require `h2`.

### P0-D HTTP limits / redirects

Client response headers are capped at 64 KiB and response bodies at 8 MiB. Redirects are never followed by MBox transport. Cronet bridge independently enforces the 8 MiB response bound and does not follow redirect callbacks.

### P1 OPEN / key hardening

Pre-auth handshakes are bounded before Noise work. Server static key is cached at startup. Noise counter exhaustion fails closed. Wire format and Noise pattern are unchanged.

### P1 provisioning confidentiality

New HTTPS App Links use `#profile=...`; legacy query links remain importable. This does not solve authenticity and is not described as doing so.

### P1 Android lifecycle

Network changes force restart/re-resolution/rebinding. Backoff is bounded and jittered. IPv6 is routed through VPN. Always-on remains disabled until the outer sockets can be protected individually rather than excluding the whole package.

## Tests run in the audit environment

### Go

PASS:

- `go test ./...` for `common/noise`, `server`, `client`;
- relay, `mbox-key`, `mbox-link`, `mbox-netprobe` compile/test successfully;
- `go test -race` for Noise and server;
- `client go test -race -count=20 ./...` PASS;
- `go vet` server/client/relay/mbox-netprobe PASS.

### Python

`15 passed`.

### Fuzz smoke

5-second smoke runs PASS:

- `FuzzDownRequestMeta` (~105k executions in this run);
- `FuzzMuxFrameParser` (~130k executions);
- `FuzzNoiseTransportRecord` (~142k executions).

### Android

Java/Cronet API usage was checked against current Android Cronet API documentation, including `bindToNetwork`, request-finished metrics/listeners and negotiated-protocol APIs. A full Android Gradle compilation cannot be honestly claimed from the audit container because it lacks the Android SDK/cached build environment. The final branch must therefore be built on the user's Arch host with JDK 17 before commit/release.

Required command:

```bash
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
./android/tools/build-debug.sh
```

This build is a release gate, not optional documentation.

## Backward compatibility

No MBox wire-format or Noise-pattern change is made.

Compatible:

- existing client IDs, PSKs and server static keys remain valid;
- old `MBOX_CONNECT_IP` is still understood;
- legacy provisioning query links still import;
- server/client beta.2 wire protocol remains compatible;
- Go HTTP mode can still be selected for troubleshooting.

Behavior changes requiring qualification:

- newly generated Android profiles default to `cronet` outer HTTPS;
- redirects are denied;
- Android now routes IPv6 through TUN;
- new App Links use fragments;
- Always-on is explicitly not advertised;
- pre-auth OPEN floods can be camouflage-denied;
- bundled Cronet increases APK size materially;
- QUIC is disabled initially even though Cronet supports it.

## Open items after this patch

1. Exact MegaFon failure layer — field test required.
2. Chrome vs Cronet-MBox vs Go-MBox capture on the same phone/network/endpoint.
3. Provisioning authenticity/trust-anchor design.
4. Multiple independent hostnames/endpoints, not just several IPs for one hostname.
5. Full Always-on/lockdown support via socket-specific protection.
6. GitHub Actions immutable-SHA pinning and Gradle wrapper-JAR provenance cleanup.
7. 24h/7d soak, Wi-Fi/mobile chaos, 10/50/100-client qualification and longer fuzzing.
## Post-audit real-device follow-up

After the original static/code audit, the branch was built and installed on a real Android device. Follow-up testing confirmed Cronet+HTTP/2 on the live outer path, exposed and fixed a self-triggered VPN network-callback loop, added wait-for-underlying-network behavior, and exposed stale-session accumulation after airplane-mode hard disconnects. Stable authenticated `device_id` session supersession fixed the latter. Stream cleanup was observed under real app load; the measured peak reached 64 streams, so the default per-session ceiling was calibrated to 128.

These follow-up checks still do not constitute a white-list-mode operator qualification or a browser-fingerprint equivalence test.
