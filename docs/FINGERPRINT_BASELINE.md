# Android HTTPS and QUIC fingerprint baseline — beta.4.1

[Русский](ru/FINGERPRINT_BASELINE.md) · [Camouflage limitations](TRAFFIC_CAMOUFLAGE_LIMITS.md)

## What is implemented

The public **HTTPS path** uses Chromium-derived bundled Cronet and HTTP/2; Go handles Noise/MBox and bridges authenticated local HTTP operations to Cronet. The **QUIC path** is a distinct native QUIC/HTTP3 UDP gateway and does not use the REG.RU HTTPS frontend. Neither path is byte-for-byte identical to a browser by design or by evidence.

Early maintainer beta.4 debug testing established Cronet HTTP/2 (`alpn=h2`) and an authenticated Noise connection. This is evidence of stack selection, **not** a Chrome/Firefox JA4 comparison. Older beta.2 Go TLS traces and earlier claims about QUIC being disabled are historical, not the beta.4.1 transport architecture.

## Baseline experiment — still pending

On the **same device, network and controlled endpoint**, compare (a) Chrome or Firefox, (b) beta.4.1 Cronet HTTPS, and (c) beta.4.1 QUIC. Record protocol, address family, TLS ClientHello/visible SNI/ALPN, negotiated QUIC version, connection lifetime/reuse, packet size distribution, timing and errors. Encrypted HTTP/2 SETTINGS require endpoint-side instrumentation or a trusted test server; do not infer them from a passive pcap. For REG.RU, separately measure the HTTPS client→frontend and backend frontend→origin flow where authorized.

Store test date, Android/API/version, APN/provider, MBox mode, source commit and exact APK hash; sanitize all logs. No realistic experiment should claim identical browser fingerprint unless measured. Compare under active network restrictions only where authorized.
