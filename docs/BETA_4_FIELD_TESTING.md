# Beta 4 field testing

[Русский](ru/BETA_4_FIELD_TESTING.md) · [Network matrix](NETWORK_COMPATIBILITY.md)

Observed on the maintainer's **debug** Android client: native QUIC over Wi-Fi, HTTPS over restricted MTS/MegaFon, Auto/failover, Telegram and repeated reconnects. The **signed release** APK passed installation and version verification, but an independent complete release-transport matrix is pending. Tele2/T2 on another phone, physical leak capture and long soak are not yet verified.

Please report network, mode, phone/Android version, timing and sanitized log stages to [GitHub Issues](https://github.com/RegSirius06/mbox/issues). Never publish a complete `mbox://` profile, PSK, server secrets, or raw sensitive logs.
