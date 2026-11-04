# UDP policy and acceptance boundary (experimental post-beta.2.2 client)

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
compatibility with the server-side SOCKS5 service and beta.2.1 record format.
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
   Preserve the existing beta.2.1 profile as a rollback candidate.
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
