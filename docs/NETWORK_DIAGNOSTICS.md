# Network diagnostics

<!-- MBOX-BETA4.1-ACTIVE-CONTEXT -->
> **Current beta.4.1 context:** The client checks authenticated Noise session health and can recover QUIC without restarting the VPN; see [current status](BETA4_1_STATUS.md), [transport/camouflage limits](TRAFFIC_CAMOUFLAGE_LIMITS.md), and [test evidence](TESTING.md). Older beta.2/3/4 observations in this document are historical, not proof of current P0 leak safety.

[Русская версия](ru/NETWORK_DIAGNOSTICS.md)

MBox 1.0.0-beta.2+ can emit stage-oriented connection diagnostics without logging provisioning secrets, PSKs, private keys, request bodies, or full URLs.

Normal mode keeps IP addresses and session IDs hashed. A provisioning link created with:

```bash
sudo mboxctl user link USER --network-debug
```

sets `diagnostics.network_debug=true` and allows full endpoint IPs in Android/core diagnostic logs. Use that only for short troubleshooting captures.

## Establishment stages

Two public HTTPS stacks are available on Android. `cronet` is the production/default fingerprint mode; `go` is the diagnostic control.

Go-stack successful sequence:

```text
NET stage=DNS code=DNS_OK ...
NET stage=TCP code=TCP_CONNECT_START ...
NET stage=TCP code=TCP_CONNECT_OK ...
NET stage=TLS code=TLS_HANDSHAKE_START
NET stage=TLS code=TLS_OK version=TLS1.3 alpn=h2 ...
NET stage=HTTP code=HTTP_OK stack=go proto=HTTP/2.0 status=200 ...
NET stage=MBOX_OPEN code=MBOX_OPEN_OK
NET stage=NOISE code=NOISE_OK
NET stage=TUNNEL code=TUNNEL_ESTABLISHED ...
```

Cronet-stack successful sequence:

```text
NET stage=DNS code=DNS_DELEGATED stack=cronet  # normal mode; Cronet resolves internally
NET stage=HTTP code=HTTP_STACK stack=cronet version=...
NET stage=CRONET code=CRONET_OK proto=h2 reused=... dns_ms=... connect_ms=... tls_ms=...
NET stage=HTTP code=HTTP_OK stack=cronet proto=HTTP/2.0 status=200 tls=cronet alpn=h2
NET stage=MBOX_OPEN code=MBOX_OPEN_OK
NET stage=NOISE code=NOISE_OK
NET stage=TUNNEL code=TUNNEL_ESTABLISHED ...
```

Cronet owns its own DNS/address racing internally. NORMAL Cronet mode deliberately does not perform an extra Android pre-resolution, avoiding a duplicate DNS lookup. With `--network-debug`, Android additionally resolves and logs A/AAAA before starting Cronet so the operator experiment has explicit answers; RequestFinished metrics still expose Cronet DNS/connect/TLS timing and reuse. If you specifically need exact per-IP attempt logs, provision `--android-http-stack go --network-debug` for that A/B run.

Important reason codes include:

- `ERR_DNS`, `ERR_DNS_EMPTY`, `ERR_DNS_NO_NETWORK`
- `ERR_TCP_TIMEOUT`, `ERR_TCP_REFUSED`, `ERR_TCP_UNREACHABLE`, `ERR_TCP_CONNECT`
- `ERR_TLS_HANDSHAKE`, `ERR_TLS_CERT`
- `ERR_HTTP_REQUEST`, `ERR_HTTP_STATUS`, `ERR_HTTP_BODY`, `ERR_HTTP_BODY_TOO_LARGE`
- `ERR_ANDROID_BRIDGE`, `ERR_ANDROID_BRIDGE_AUTH`, `ERR_ANDROID_BRIDGE_PROTOCOL`
- Cronet: `ERR_NETWORK_DISCONNECTED`, `ERR_NETWORK_CHANGED`, `ERR_TCP_RESET`, `ERR_CRONET_OTHER`
- `ERR_NOISE_AUTH`
- `ERR_HANDSHAKE_LIMIT` on the server
- Android lifecycle: `UNDERLYING_LOST`, `WAIT_UNDERLYING`, `UNDERLYING_AVAILABLE`, `UNDERLYING_CHANGED`
- reconnect reason: `RECONNECT reason=underlying_network immediate=true` for a real physical-network transition

## Android capture

```bash
adb logcat -c
adb logcat -v time MBox:I '*:S'
```

Do not publish a debug capture without reviewing it. In network-debug mode endpoint IP addresses are intentionally visible.
