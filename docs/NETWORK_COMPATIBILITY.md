# Network compatibility — beta 4

[Русский](ru/NETWORK_COMPATIBILITY.md) · [Field testing](BETA_4_FIELD_TESTING.md)

| Network | Observed behavior | Evidence level |
|---|---|---|
| Unrestricted Wi-Fi | Direct QUIC and Auto with reconnect work | Debug APK, maintainer device |
| MTS restricted network | HTTPS/Cronet works, Telegram accessible | Debug APK, maintainer device |
| MegaFon restricted network | HTTPS/Cronet works, Telegram accessible | Debug APK, maintainer device |
| Tele2 / T2 | Not yet checked on second device with beta 4 release APK | Pending |

These are observations of one deployment, not blanket compatibility guarantees. Network policy varies by carrier, area, APN, hostname, time and subscriber profile. The signed release APK was installed; complete transport test results from that APK are not yet recorded. The old `.fun` vs `.ru` hostname hypothesis belongs to earlier field history and is **not** proof of an operator bypass.

Collect sanitized stage logs (DNS → TCP/UDP reachability → TLS/QUIC → MBox/Noise → TUN → app requests). Never upload full profiles or keys.
