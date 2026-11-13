# MBox 1.0.0-beta.4.1 — source and release acceptance status

[Русский](ru/BETA4_1_STATUS.md) · [Testing](TESTING.md) · [Release checklist](../RELEASE_CHECKLIST.md)

**Candidate, not automatically published.** Based on `v1.0.0-beta.4`; no server wire-protocol or `mbox://` format change. The author must verify the exact final signed artifact and attach its actual SHA-256 before publishing. No server update is needed solely for Noise framePing liveness; certificate rotation remains an independent operator task.

## Source-confirmed bug and fix

- beta.4 Android set CONNECTED after local SOCKS startup and waited for the Go core process to exit. A dead QUIC gateway could be overlooked while the core stayed alive.
- beta.4 server already echoes the existing authenticated Noise `framePing`. beta.4.1 uses a random 16-byte request nonce, one pending reply and a 10 s deadline to verify the remote session without a new wire frame.
- Core probes are spaced **30 s while streams are active, 90 s while idle**, plus up to 30% timing jitter. A screen wake/Doze-exit requests an extra probe. These intervals are policy, not a hard battery or recovery-time guarantee under Android scheduling restrictions.
- Android shows CONNECTING/VERIFYING/CONNECTED/RECOVERING, monitors native QUIC gateway and HEV independently, and normally retains the full-route TUN during transport recovery.

## Evidence to date

| Evidence | Outcome | Limits |
|---|---|---|
| Go client and server `go test -race` on Arch | Reported passed | Exact final commit not yet checked in GitHub CI |
| Native QUIC `go vet` and `go test -race` on Arch | Reported passed | Does not replace device-network testing |
| Python suite on initial patch | 114 passed, 1 static-spacing assertion failed | Assertion correction supplied; final all-green result not included in the received logs |
| ARM64 Go core and native QUIC build | Reported passed | Final APK must be rebuilt from exact commit |
| Android debug APK | Reported built successfully | No final production-signed acceptance record |
| Connection, screen-off idle, network switch, complete outage, Auto | Maintainer reported 5/5 passed | Durations, detailed network conditions, build signer and logs were not provided |

## Still not confirmed

- Original idle hang's real physical root cause (NAT, Doze, packet loss or gateway failure): source logic defect is confirmed, physical cause is not.
- Synthetic alive-but-stalled QUIC, forced gateway/core/HEV kill, QUIC blocked with HTTPS reachable, both paths blocked, user stop during recovery, repeated callback races and 24–72 h soak.
- Physical capture verifying IPv4, IPv6, DNS, TCP and UDP behavior with Always-on and Android Lockdown, including full app process death.
- Browser/TLS/HTTP fingerprint comparison and frontend→origin traffic analysis, throughput/jitter/power measurements.
- In-place upgrade and full smoke test on the **same final production-signed APK** for public release; server CA migration and certificate renewal on VPS.

**P0 physical no-leak acceptance remains open.** Always-on supports persistence; only Android's Lockdown is the OS-level block-on-VPN-absence mechanism. Do not claim a third-party audit or universal concealment.
