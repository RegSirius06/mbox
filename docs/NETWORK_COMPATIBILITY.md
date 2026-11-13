# Network compatibility — beta.4.1

[Русский](ru/NETWORK_COMPATIBILITY.md) · [Testing](TESTING.md)

These are **deployment-specific observations**, not guarantees for every carrier, network, DNS or mobile plan.

| Source | Scenario | Evidence |
|---|---|---|
| beta.4 maintainer debug tests | QUIC over unrestricted Wi-Fi, HTTPS/Cronet on restricted MTS/MegaFon, Telegram | Historical maintainer observations, not beta.4.1 acceptance |
| beta.4.1 maintainer report | Normal connect, screen-off idle, underlying-network handover, total network outage, Auto | Reported passed (5/5); device model, operator, elapsed time, precise fallback condition and log unavailable |
| beta.4.1 final production-signed APK | Same complete matrix, T2/Tele2 on second phone, physical leak capture, long soak | Not yet verified |

Auto *being selected* does not prove QUIC→HTTPS fallback unless UDP was blocked and HTTPS remained reachable. QUIC direct VPS uses UDP and never goes through the REG.RU frontend. HTTPS uses Cronet HTTP/2 and can traverse shared hosting. Preserve sanitized stage logs and conditions when filing issues; never publish a full `mbox://` link.
