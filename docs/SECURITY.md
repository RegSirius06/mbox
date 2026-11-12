# MBox security boundaries — beta 4

[Русский](ru/SECURITY.md) · [Android guide](ANDROID_BETA4.md) · [Test plan](ANDROID_FAIL_CLOSED_TEST_PLAN.md)

**Experimental software; not independently security-audited.** Successful APK signing, a passing test suite, and a working VPN status do not prove absence of traffic leaks.

### Protections in the implementation

- MBox application traffic uses Noise authenticated encryption with server identity and per-profile secrets. The outer HTTPS/QUIC layer does not replace Noise.
- DOWN replay/ACK and bounded queues, session and stream limits guard important loss and resource failure modes; not every long soak or churn scenario is proven.
- Android uses `VpnService`/TUN. The intended design routes application traffic only through the MBox core. Only protected/bound outer transport sockets should use the physical network.
- The release APK is signed with a persistent certificate. Expected certificate SHA-256: `17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586`. This is the **publisher** certificate, not a QUIC trust anchor.
- Release APK does not bundle the operator's QUIC endpoint; trusted QUIC TLS certificate is imported per profile.

### Remaining risks

- Physical-interface captures and kill/crash/handover validation for DNS, TCP, UDP, IPv4 and IPv6 across devices/carriers are still outstanding. Enable Android **Always-on VPN** and **Block connections without VPN** and test them. Do not claim certified fail-closed behavior based on UI state alone.
- `mbox://` links carry reusable secret credentials. Do not share them, raw logs, private keys, provisioning exports, or real backend config in public issues.
- The ISP observes the outer destination/protocol, timing and packet sizes. HTTPS frontend can observe request metadata; the origin handles authenticated decrypted traffic. Cronet reduces a particular implementation fingerprint but is not perfect browser impersonation or DPI immunity.
- Session identity, long-lived provisioning/PSK, frontend DoS boundaries, supply-chain reproducibility and independent audit are ongoing work.
- The test QUIC server certificate expires **2026-12-07**. Renew it and distribute an authenticated trust update, never an insecure TLS fallback.

Security reports should minimize sensitive network data and use private disclosure channels when needed.
