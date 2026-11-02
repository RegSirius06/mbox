# sing-box routing handoff

[Русская версия](ru/SINGBOX.md)

MBox is a transport from the client device to the MBox server. It deliberately does **not** decide where Internet traffic leaves after it reaches the server.

The default server setting:

```ini
[server]
upstream = 127.0.0.1:2080
```

means: for every MBox TCP stream, connect to a local SOCKS/HTTP-capable proxy endpoint at `127.0.0.1:2080`.

The recommended deployment uses a **sing-box `mixed` inbound** on that address. A mixed inbound accepts SOCKS4/SOCKS5/HTTP proxy connections. MBox connects to it locally; sing-box then applies its normal routing rules and chooses any configured outbound.

```text
Android / desktop
      |
      | MBox over HTTPS/shared-host frontend
      v
mbox-server 127.0.0.1:18449
      |
      | SOCKS to 127.0.0.1:2080
      v
sing-box mixed inbound: mbox-router-in
      |
      +--> direct
      +--> Hysteria2 server
      +--> VLESS/REALITY server
      +--> Shadowsocks / SOCKS / SSH tunnel
      `--> any other sing-box route you configure
```

MBox itself does not care whether the final exit is the same RU VPS, Finland, Poland, Denmark, or another chained server. The REG.RU website/relay is also independent from this routing decision: it lives on shared hosting and only forwards the MBox HTTP transport to the configured origin.

## Minimal example

A complete example is in `examples/sing-box-router.json`. The important part is:

```json
{
  "inbounds": [
    {
      "type": "mixed",
      "tag": "mbox-router-in",
      "listen": "127.0.0.1",
      "listen_port": 2080
    }
  ]
}
```

Then set `route.final` (or explicit route rules) to the outbound you want.

For a direct exit from the same VPS:

```json
{
  "outbounds": [
    { "type": "direct", "tag": "direct" }
  ],
  "route": {
    "final": "direct",
    "auto_detect_interface": true
  }
}
```

For a chained foreign exit, replace `route.final` with your Hysteria2/VLESS/etc. outbound tag. Keep private destinations/direct infrastructure routes explicit so the tunnel does not recursively route itself.

## Why `127.0.0.1`

Keep the mixed inbound on loopback unless another host genuinely needs to reach it. MBox and sing-box run on the same machine in the normal deployment, so exposing port 2080 publicly only adds attack surface.

## What MBox transports

MBox currently multiplexes TCP streams. The `mixed` inbound is therefore used as a local TCP proxy handoff. UDP/QUIC is not a native MBox transport yet; applications may fall back to TCP depending on their own behavior.
