# MBox release status

[Русская версия](ru/STATUS.md)

## Current: `1.0.0-beta.3` (experimental prerelease, 2026-11-05)

- **Deployed as code on one test setup:** Android + root-managed RU VPS + existing REG.RU FastCGI; cellular data worked during active allow-list restrictions and Telegram loaded.
- **Compatibility:** published beta.2.1 Android lacks `cmac`; origin with `auth.require_authenticated_control=true` rejects it. Noise record suite and profile/PSK bytes are unchanged. beta.2.2 clients built with `cmac` remain compatible.
- **Network:** Android full-route TUN, Cronet-based HTTPS/H2, TCP-over-Noise, local SOCKS rejects UDP ASSOCIATE/BIND; native UDP is not implemented.
- **Not accepted:** physical-interface DNS/TCP/UDP/IPv6 leak captures, Android crash/Lockdown, 24-72h soak, high-concurrency relay testing, one-time signed enrollment and Chrome/Cronet fingerprint regression.
- **Release policy:** no debug-signed APK in public GitHub release; pin release signing certificate and publish checksum. Experimental prerelease must not be presented as a production-security audit.

See `../RELEASE_NOTES.md`, `BETA_3_RELEASE_STATUS.md`, `ANDROID_FAIL_CLOSED_TEST_PLAN.md`.

---

## Historical beta.2.1 status (2026-11-02)

## 1.0.0-beta.2.1 (historical)

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

### At-the-time open items for beta.2.1

- field tests during active mobile white-list restrictions on MTS/T2/MegaFon;
- controlled `.fun` vs `.ru` test on the same shared-host backend;
- Chrome vs Cronet-MBox vs Go-MBox packet fingerprint capture;
- provisioning authenticity/trust-anchor design;
- true Android Always-on/lockdown support;
- multiple independent frontend hostnames/endpoints;
- longer soak/load/fuzz qualification;
- independent external security review.

The current branch is experimental. No claim of exact Chrome impersonation, universal DPI bypass or production/security audit is made.
