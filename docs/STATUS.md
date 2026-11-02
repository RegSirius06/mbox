# MBox release status

[Русская версия](ru/STATUS.md)

## 1.0.0-beta.2.1 (current version)

`1.0.0-beta.2.1` is a beta.2-compatible hardening release candidate/current tree. It preserves the beta.2 Noise wire and credentials while replacing Android public HTTPS with bundled Chromium Cronet and adding the diagnostics/lifecycle/resource hardening below.

## 1.0.0-beta.2 (previous tagged baseline)

`1.0.0-beta.2` established the Noise/replay/ACK/flow-control baseline. Its Android public HTTPS path used Go `crypto/tls` + `net/http`.

## beta.2.1 hardening / field-test delta

The current branch keeps the beta.2 Noise wire generation and adds:

- stage diagnostics: DNS -> TCP -> TLS -> ALPN -> HTTP -> OPEN -> Noise -> tunnel;
- bounded multi-address fallback in the Go diagnostic path;
- dual-stack Android TUN routing;
- Chromium Cronet as the default Android public HTTPS stack, with HTTP/2 enabled and QUIC disabled;
- authenticated loopback Go<->Cronet bridge and `cronet`/`go` A/B modes;
- non-VPN underlying-network monitoring and Wi-Fi/mobile handover recovery;
- airplane-mode wait-for-network behavior without blind retries;
- stable per-installation `device_id` inside authenticated Noise OPEN and same-device stale-session supersession;
- pre-auth OPEN rate/concurrency limits, bounded client responses and redirect denial;
- provisioning secret moved from HTTPS query to fragment;
- modularized client/server source layout with CI file-size guards;
- calibrated `max_streams_per_session=128` default.

### Real-device checks completed on 2026-11-02

The development build was built and installed on a real ARM64 Android device. Logs confirmed:

- `stack=cronet`;
- HTTP/2 (`alpn=h2`);
- `MBOX_OPEN_OK`;
- `NOISE_OK`;
- `TUNNEL_ESTABLISHED`;
- ordinary Telegram/Instagram/video traffic;
- Wi-Fi/mobile handover;
- airplane-mode hard disconnect -> `WAIT_UNDERLYING` -> recovery;
- server `SESSION SUPERSEDE` of stale sessions from the same device;
- stream counts falling after idle, with a measured stress peak of 64 concurrent streams.

### Still open before finalizing/tagging beta.2.1

- field tests during active mobile white-list restrictions on MTS/T2/MegaFon;
- controlled `.fun` vs `.ru` test on the same shared-host backend;
- Chrome vs Cronet-MBox vs Go-MBox packet fingerprint capture;
- provisioning authenticity/trust-anchor design;
- true Android Always-on/lockdown support;
- multiple independent frontend hostnames/endpoints;
- longer soak/load/fuzz qualification;
- independent external security review.

The current branch is experimental. No claim of exact Chrome impersonation, universal DPI bypass or production/security audit is made.
