# UDP transport — beta 4

[Русский](ru/BETA_4_TRANSPORTS.md) · [Transport matrix](BETA_4_TRANSPORTS.md) · [Leak test](ANDROID_FAIL_CLOSED_TEST_PLAN.md)

**Current behavior:** MBox beta 4 supports application UDP datagrams inside authenticated/encrypted MBox transport records. `enable_udp` enables the normal client path independently of diagnostic developer mode. The prior beta.2 fail-closed policy rejected UDP ASSOCIATE while UDP forwarding was unavailable; that historical interim restriction must **not** be described as the beta 4 implementation.

### Outer paths

- HTTPS/Cronet over TCP: UDP application datagrams are tunneled through encrypted MBox requests over HTTP/2/TCP. Underlying network transport is TCP, not UDP.
- QUIC/HTTP3 over UDP: QUIC uses a real UDP socket to the configured VPS gateway; MBox content uses HTTP/3 **streams**, not QUIC DATAGRAM.
- Both modes retain MBox authentication and server-side routing to the intended exit; arbitrary application direct UDP fallback is not part of the design.

### Security limitation

Real-device physical Wi-Fi/cellular captures are still required to prove that no application traffic escapes during TUN lifecycle failures, DNS/IPv6 handling or network handover. Enable Android Always-on VPN and Lockdown. Do **not** disable TLS verification to work around the QUIC server certificate expiry.

For the full protocol and operational model, see [Transport guide](BETA_4_TRANSPORTS.md).
