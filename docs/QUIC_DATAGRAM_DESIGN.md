# QUIC DATAGRAM — possible beta.5 design, NOT in beta.4.1

[Русский](ru/QUIC_DATAGRAM_DESIGN.md) · [Transport behavior](BETA_4_TRANSPORTS.md)

QUIC DATAGRAM (RFC 9221) carries encrypted, congestion-controlled **unreliable** messages that are not retransmitted by QUIC. The beta.4.1 native gateway instead forwards authenticated MBox HTTP transactions using **reliable HTTP/3 streams**. Thus application UDP remains inside the reliable stream. It is not equivalent to native datagrams, and it does not remove all head-of-line effects in the nested application protocol.

For a later release, investigate a separate authenticated datagram association (HTTP Datagrams / CONNECT-UDP or explicit extension), replay/nonce discipline, flow/byte quotas, MTU and fragmentation, loss/reordering, backpressure, Android protected sockets, fallback to HTTPS and server migration. This is **not** a drop-in configuration flag. It requires coordinated client/gateway changes, compatibility negotiation and real-network testing. Do not include it in the beta.4.1 feature list.

References: https://www.rfc-editor.org/rfc/rfc9221.html and https://quic-go.net/docs/http3/datagrams/.
