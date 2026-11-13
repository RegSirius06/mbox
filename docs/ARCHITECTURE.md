# MBox architecture (beta.4.1)

<!-- MBOX-BETA4.1-ACTIVE-CONTEXT -->
> **Current beta.4.1 context:** The client checks authenticated Noise session health and can recover QUIC without restarting the VPN; see [current status](BETA4_1_STATUS.md), [transport/camouflage limits](TRAFFIC_CAMOUFLAGE_LIMITS.md), and [test evidence](TESTING.md). Older beta.2/3/4 observations in this document are historical, not proof of current P0 leak safety.

[Русский](ru/ARCHITECTURE.md) · [Transports](BETA_4_TRANSPORTS.md)

```text
Android apps -> VpnService/TUN -> HEV -> local Go MBox SOCKS -> Noise/MUX
                                                        |
                             +--> QUIC/HTTP3 over UDP --+--> VPS QUIC gateway --> origin --> sing-box --> exit
                             |
                             +--> Chromium Cronet HTTPS/HTTP2 over TCP --> shared hosting FastCGI --> origin
```

The **frontend**, **origin**, and **exit** can run on separate providers and in separate countries. REG.RU shared hosting serves a regular website and FastCGI relay; it is **not** the RU VPS. A direct QUIC route bypasses FastCGI. An origin may forward through sing-box to a separate exit.

**Application UDP:** SOCKS UDP/flow datagrams traverse authenticated MBox data records on either path. HTTPS uses TCP externally; QUIC uses UDP externally but HTTP/3 streams internally. Do not describe either as QUIC DATAGRAM or transparent direct UDP from the application to the destination.

**Auto:** tries QUIC first when configured, falls back to HTTPS on failed connection and reevaluates after network change. Existing connections can reset; this is not seamless migration/multipath or continuous performance racing.

**Profile:** `mbox://` contains client credentials and transport configuration. `transport.quic_url` and `transport.quic_ca_pem` are required together, and are omitted for HTTPS-only clients. The APK never contains the operator's endpoint/certificate. Keep private profiles out of source and issue reports.

**beta.4.1 recovery:** existing authenticated Noise framePing/pong, gateway/HEV health watch, VPN CONNECTING/VERIFYING/RECOVERING, and TUN-preserving reconnect. See [status](BETA4_1_STATUS.md).

**Security and reliability:** Noise authentication/encryption, DOWN replay/ACK, quotas/bounded queues, Android system VPN and optional Lockdown. These mechanisms do not replace packet-capture verification, independent auditing or documented operational limits.
