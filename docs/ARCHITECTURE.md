# MBox architecture — 1.0.0-beta.4.4

[Русский](ru/ARCHITECTURE.md) · [Subscriptions](MBOX_SUBSCRIPTIONS.md) · [DATAGRAM](BETA44_DATAGRAM_EXPERIMENT.md) · [Security](SECURITY.md)

```text
Android apps -> VpnService/TUN -> HEV -> Go core / Noise / MUX
                                              |
               +--> QUIC/HTTP3/UDP -----------+--> VPS native QUIC gateway --> origin -----+
               |                              |                                           |
               +--> HTTPS/Cronet/TCP ---------+--> shared-host FastCGI -----> origin -----+--> sing-box SOCKS5 exit (DK, KZ, ...)

On QUIC: real application UDP -> separate QUIC DATAGRAM lane when ready; otherwise MBox STREAMS
On HTTPS: application UDP -> encrypted MBox STREAMS, externally carried by TCP
TCP/control: reliable MBox STREAMS on either outer transport
```

**Ingress vs exit:** QUIC and FastCGI are independent *entry paths*, not separate user subscriptions or countries. Origin authenticates Noise sessions and decides an allowlisted SOCKS5 route. A default route is kept for beta.4.3/older MBD1 clients; beta.4.4 uses authenticated MBD2 `egress_id` to request named exits. One canonical subscription may produce profiles for several exits. The same origin remains a single point of failure.

**Transport AUTO** prefers QUIC, switches to HTTPS after network/transport failure; this can reset active streams. **UDP AUTO** is distinct: if the DATAGRAM lane is up and payload fits it, datagrams are sent there; otherwise new UDP data use reliable MBox streams. Replays/duplicate datagram packets and bounded queues are handled in the DATAGRAM layer; encrypted association OPEN/CLOSE remain in reliable streams. An idle dedicated lane may time out after about 30s and reattach on real UDP demand with independently derived AES-GCM key/nonce domains per epoch. No periodic DATAGRAM PING; not seamless multipath.

**Noise formats:** session encryption and record-v2 are separate from MBD1/MBD2 authenticated OPEN metadata. MBD2 is not an unauthenticated HTTP query and never lets the client specify an arbitrary SOCKS5 address. The server validates an `egress_id` against its configured local allowlist.

**Security/operations:** authenticate and authorize at origin; cap sessions, streams, queues and UDP traffic; preserve replay windows. Android should fail closed when physical connectivity is unavailable. Independent packet-capture/Lockdown, long-soak, battery and FastCGI CPU tests are still required; a successful field connection is not an assurance of leak-freedom.
