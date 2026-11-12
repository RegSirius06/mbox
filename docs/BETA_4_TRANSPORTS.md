# MBox beta 4 — UDP and transport architecture

[Русский](ru/BETA_4_TRANSPORTS.md) · [Beta 4 release notes](BETA_4_RELEASE_NOTES.md)

**Status: development candidate, not a certified production VPN.** The published beta 3 and the local beta 4 development tree are different builds. A working debug APK does not imply the signed release variant contains native QUIC.

## Why there are two outer transports

MBox tunnels application TCP streams and, in the beta 4 development branch, application UDP datagrams. The application-facing VPN interface and MBox's Noise-authenticated session are separate from the *outer transport* used to reach the server.

### HTTPS through an HTTP intermediary

```text
Apps (TCP / UDP)
  -> Android VpnService / TUN -> HEV / MBox client
  -> MBox encrypted frames + reliable HTTP request/response protocol
  -> Chromium Cronet HTTPS (typically HTTP/2 over TLS/TCP)
  -> publicly reachable shared-hosting frontend + FastCGI relay
  -> MBox origin VPS -> configured exit (e.g. sing-box)
```

Application UDP is carried as framed datagrams **inside the MBox tunnel**, which itself travels over HTTPS/TCP. This is UDP tunnelling, **not native UDP on the public ISP-to-frontend connection**. A loss on the outer TCP path can introduce head-of-line blocking and latency, especially for real-time traffic. The HTTPS intermediary must actually be reachable on the restricted network; there is no universal allow-list bypass guarantee.

### Direct native QUIC / HTTP/3

```text
Apps (TCP / UDP)
  -> Android VpnService / TUN -> HEV / MBox client
  -> Noise-authenticated MBox frames in HTTP/3 streams
  -> direct QUIC over UDP -> MBox native QUIC gateway on VPS
  -> local MBox origin -> configured exit
```

This route does **not** use the shared-hosting FastCGI frontend. QUIC's outer packets use UDP, and the current implementation uses **HTTP/3 streams**, not QUIC DATAGRAM frames. Do not describe it as loss-independent end-to-end native datagram delivery or as QUIC multipath. QUIC will fail where its destination UDP is blocked.

## Android Auto mode

Auto selects a transport for each connection/reconnection attempt. It prefers direct QUIC when usable and falls back to HTTPS when QUIC fails. The maintainer observed QUIC on Wi-Fi and automatic HTTPS reconnection on mobile networks with active access restrictions, then QUIC again on Wi-Fi. This is **connection-level failover**, not simultaneous per-flow load balancing, bonding or seamless stream migration. Existing app flows can be interrupted when a session changes.

Manual HTTPS and manual QUIC modes are retained for diagnosis.

## What is encrypted and what remains visible

MBox uses Noise `NKpsk0` for authenticated session encryption; the outer HTTPS/QUIC transport also has its own TLS protection. Cronet provides a Chromium-derived TLS/HTTP implementation for the HTTPS route. This is **not** proof of being identical to Chrome/Firefox or of resisting every DPI classifier. Observers can still see endpoint IPs, timing, packet sizes and transport type. Cover traffic, when enabled, does not prove unlinkability or leak freedom.

## Known limitations

- No QUIC DATAGRAM transport, multipath, path bonding or transparent continuity of existing streams after failover.
- HTTPS/TCP can be slow for latency-sensitive UDP and is subject to HTTP intermediary behavior.
- Comprehensive physical-interface DNS/IPv4/IPv6 leak verification, long-duration multi-user soak and independent security audit remain outstanding.
- QUIC gateway certificate renewal and distribution must be handled before expiry; the current laboratory certificate was reported to expire on **2026-12-07**.
- Native QUIC and Auto were validated in a **debug Android build**; the beta 4 release build must include and exercise both before publication. The current lab configuration embeds a deployment-specific QUIC destination and public certificate in **debug assets**; a general release must obtain appropriate endpoint/trust material via per-user configuration or another reviewed distribution method instead of silently bundling the maintainer's private deployment endpoint.
- Binary/profile compatibility with previous experimental betas is not guaranteed.

See [field testing and issue reports](BETA_4_FIELD_TESTING.md) and [platform roadmap](PLATFORM_ROADMAP.md).
