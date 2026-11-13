# MBox native QUIC / HTTP3 — beta.4.1

[Русский](README.ru.md) · [Android](../docs/ANDROID_BETA4_1.md) · [TLS rotation](../docs/QUIC_TLS_ROTATION.md)

The native QUIC module implements **HTTP/3 over UDP** for existing authenticated MBox HTTP/Noise records. The Android gateway exposes a private Unix HTTP socket to the local Go core and obtains protected outbound UDP sockets from the Android broker. QUIC DATAGRAM is **not** used: application UDP remains nested in reliable HTTP/3 streams.

- `serve`: additional VPS QUIC listener, path allowlist, signed authenticated forwarding to loopback origin. Deployment requires an operator's separate authorization; no gateway or origin update is needed solely for beta.4.1 framePing.
- `client`: protected UDP to numeric IPv4 endpoint configured per profile; standard TLS 1.3 + X.509 chain, expiry and endpoint IP SAN checks; no `InsecureSkipVerify`.
- QUIC availability is **not** evidence of MBox session health. The Go core verifies an authenticated Noise ping/pong. Auto may fall back to Cronet HTTPS with an application-flow reset.

```bash
cd native-quic
go vet ./...
go test -race -count=1 ./...
```

For operator TLS CA/leaf trust, rollback, and test migration use [the runbook](../docs/QUIC_TLS_ROTATION.md). Existing beta.4 signed-leaf profiles may need an authenticated one-time reimport before the December 7, 2026 expiry. The operator must not enable the renewal timer automatically while reviewing a client release.
