# UDP policy and acceptance boundary (beta.3 experimental client)

MBox currently implements authenticated/encrypted **TCP streams**, not native
UDP datagrams. `hev-socks5-tunnel` supports UDP-in-UDP and UDP-in-TCP. Its
`socks5.udp: tcp` setting is **not** a UDP-disable flag. The legacy local MBox
SOCKS listener used to forward arbitrary bytes to the server-side SOCKS proxy,
so the effective command policy depended on the remote SOCKS implementation.

## Explicit local fail-closed policy

The Android/Go MBox client local SOCKS gateway now parses an RFC 1928 greeting
and request, forwarding **only** version 5, no-auth, `CONNECT` (command `0x01`)
to the remote SOCKS service. `BIND` (`0x02`), `UDP ASSOCIATE` (`0x03`) and
unknown commands return SOCKS reply `0x07` (command not supported). Unknown
address types return `0x08`. No unsupported command crosses the MBox tunnel.
The greeting and CONNECT request are forwarded byte-for-byte, retaining
compatibility with the server-side SOCKS5 service and existing Noise/TCP record format.
The handshake has a 30-second deadline, and buffers are bounded by SOCKS5
field lengths. A rejected command closes its TCP stream and releases resources.

**Limitation:** This does not make UDP work. UDP-based applications can fail or
fall back to TCP themselves. MBox must never silently let them use a physical
socket outside the encrypted tunnel. Real Android packet captures are still
required to establish that this invariant holds under Wi-Fi/LTE handover,
service crash, DNS, STUN, WebRTC, QUIC and IPv6.

The Android full-route VPN and Cronet physical-network binding remain as in
RC1. If the Android process is killed, only the **OS Always-on VPN lockdown**
policy can be relied on to prevent the OS from granting ordinary connectivity
before VPN restoration. Users must enable both "Always-on VPN" and "Block
connections without VPN" for that threat model. App code cannot turn these
settings on itself.

## Manual physical-device acceptance criteria

1. Enable Android Always-on and "Block connections without VPN". Verify
   both settings; **do not** run disruptive tests before they are enabled.
2. Confirm TCP browsing, Telegram messages/images and an IPv6-capable target.
   Preserve the existing user profile as a rollback candidate.
3. Attempt SOCKS5 UDP ASSOCIATE and BIND locally through an authorized test
   harness; they must receive `05 07` and cause **zero** outgoing UDP packets.
4. Collect client-side VPN/TUN and physical Wi-Fi/cellular packet captures,
   or use a trusted access point/router capture and a device-rooted capture.
   An unprivileged ADB log cannot establish absence of physical leaks.
5. While capturing, exercise DNS UDP/TCP, STUN, QUIC, WebRTC, Wi-Fi -> LTE,
   LTE -> Wi-Fi, Wi-Fi loss, `am force-stop`, and process restart.
6. All physical egress must be attributable to permitted MBox outer HTTPS,
   platform network-health/captive-portal traffic explicitly documented by the
   threat model, or an independently approved exception. Any unexplained
   application user payload is a **P0 failure**.
7. Test with the VPN lockdown disabled **only on an isolated test device**;
   absence of leakage is NOT guaranteed when the VPN process is terminated.

Automated Go unit tests cover the SOCKS5 parser, fragmentation, refusal and
byte-for-byte CONNECT preservation; they are not a substitute for device
packet capture or native HEV integration tests.

## UDP development milestone 1 (after beta.3; **not enabled**)

The first preparatory change introduces `common/udpwire`, a reusable bounded
codec for MBox's prospective encrypted datagram frames and HEV's proprietary
SOCKS5 FWD UDP (`CMD=0x05`) length-prefixed UDP-in-TCP messages.

- Independent IPv4 and IPv6 address forms; domain/zone/unspecified destinations
  are rejected at this stage. This parser performs **no DNS resolution**.
- One datagram per frame. Maximum payload is **1232 bytes**; no silent
  truncation, fragmentation, reassembly or direct UDP fallback.
- Versioned internal encoding includes authenticated-session-local, nonzero
  `flow_id`. In a future transport this entire payload must be inside a Noise
  record; it is never exposed in the HTTP URL or sent on the Android physical
  interface.
- Encoders/decoders copy datagram contents, enforce exact sizes and reject
  malformed headers. Bounded streaming HEV reads fail closed.
- Regression and fuzz tests cover malformed/truncated input, IPv4/IPv6,
  payload limits, canonical form, streaming partial reads and codec roundtrips.

**Crucial:** a standalone codec does not provide UDP connectivity or prove
leak freedom. The live local SOCKS client still rejects `CMD=0x05` and RFC
1928 `UDP ASSOCIATE (0x03)`. Do not change this until all security acceptance
conditions below pass.

### Planned transport handoff

```
Android HEV (udp: tcp) -> local SOCKS FWD UDP (no physical UDP)
 -> MBox encrypted datagram frame over existing Noise/HTTPS/REG.RU relay
 -> per-session bounded UDP association on origin
 -> sing-box mixed inbound SOCKS5 UDP ASSOCIATE
 -> configured sing-box routing (direct or chained exit)
 -> reply via the SAME MBox session / Noise / HEV datagram
```

Direct `net.DialUDP` from the RU VPS to an Internet destination would bypass
sing-box routing and potentially expose DNS or other traffic from the wrong
exit. It must **not** be used as a shortcut. DNS, STUN, QUIC, WebRTC and VoIP
should take the same authenticated path when UDP is eventually enabled.

Next implementation gates: (1) SOCKS5 FWD-UDP association parser with strict
SOCKS handshake, (2) session-owned UDP flow map and per-flow/global quotas,
TTL and backpressure, (3) authenticated MBox UDP frame and ordered delivery,
(4) SOCKS5 UDP-ASSOCIATE handoff to sing-box with bounded buffers, no direct
origin sockets, (5) real Android Wi-Fi/LTE/TUN packet capture with VPN Lockdown,
service crash, network handover, DNS/UDP/IPv6/QUIC/STUN, plus load, race and
long-soak tests. Keep UDP disabled until all five are confirmed.

## UDP development milestone 2 — session-owned SOCKS5 association (LAB ONLY)

The next source-only milestone adds the **complete Go-client-to-origin datagram
path**, but **does not enable it in the Android build or in default server
configurations**. It is intentionally unsuitable for release to ordinary VPN
users until real Android physical-link capture verifies fail-closed behavior.

- `transport.enable_udp_lab=true` is an explicit, opt-in key in the Go client
  JSON. Client startup additionally requires `--developer-mode`; Android does
  **not** import or enable this flag from a public profile.
- Server `transport.enable_udp_lab=true` independently gates UDP frames; the
  default is `false`. Old TCP/Noise traffic remains unchanged.
- HEV SOCKS command **0x05** is intercepted locally after its ordinary SOCKS
  greeting. The original TCP SOCKS stream closes. A new MBox flow is opened
  and its SOCKS success reply is returned **only after** the server's UDP
  association is confirmed. **0x03 UDP ASSOCIATE remains denied locally.**
- `frameUDPOpen=7`, `frameUDPReady=8`, `frameUDPData=9`; a datagram carries
  the bounded, versioned `udpwire` encoding **inside Noise**. Client and server
  verify flow ID matching, prohibit unknown frames, use the same ordered DOWN
  delivery and authenticated HTTP ACK/replay logic as the TCP path.
- The origin connects by TCP to the configured SOCKS5 upstream and requests
  RFC 1928 UDP ASSOCIATE. Its **only UDP socket is connected to the loopback
  address returned by that SOCKS relay**. DNS names and non-loopback UDP
  relays are rejected, as are unsolicited packet-source addresses. The
  remote SOCKS (normally sing-box) controls all final destination routing.
- Per-session UDP flow count ≤16; per-flow pending queue ≤32 datagrams;
  ≤128 datagrams/second per flow; ≤64 recently contacted destinations;
  UDP data ≤1232 bytes; 2-minute idle timeout; server global queue accounting
  includes all waiting UDP payloads. Socket and queue cleanup follows session
  shutdown and credential revocation. No fragmentation and no direct bypass.

**Limitations and must-pass gates before enabling for users:**

1. Validate the running sing-box version/inbound actually supports UDP
   ASSOCIATE, including its chosen overseas outbound policy; the test proxy in
   Go tests proves protocol behavior, **not** real sing-box routing.
2. Run a full HTTPS/REG.RU integration test and then a physical Android TUN,
   Wi-Fi and LTE capture under VPN Lockdown. Check UDP DNS, IPv6, STUN, QUIC,
   invalid datagrams, process death, handover, reconnect and TCP regression.
3. Run long-duration soak, socket/FD and goroutine leak checks under concurrent
   UDP flows and multiple users. Source-level `go test -race` is necessary but
   insufficient for a public release.
4. Decide on MTU/fragmentation for larger UDP and actual Android config/UI
   integration. The current 1232-byte limit intentionally drops larger UDP
   datagrams rather than truncating them or bypassing the tunnel.

**Do not deploy this milestone to the production Android VPN or enable the
lab setting on a public origin.** The validated beta.3 remains TCP-only.
