# Physical IPv4/IPv6/DNS/UDP leak and Android Lockdown matrix (beta.4.4)

**Severity:** high validation gap (P1), not a confirmed exploitable vulnerability. **Component:** Android VPN + QUIC/HTTPS.

Go/unit tests and application behavior do not prove physical-interface leak-freedom. Test fresh boot, permission grant/revoke, Always-on/Lockdown combinations, no physical network, captive portal, Wi-Fi/mobile handover, screen-off, process kill, QUIC UDP blocked with HTTPS recovery, QUIC DATAGRAM fallback and DATAGRAM-only UDP drops. Capture packets on the physical interface or a trusted router and verify no application data/DNS escapes the VPN; only protected outer transport traffic is expected. Include IPv6 and system DNS. Redact addresses, credentials and subscriptions.
