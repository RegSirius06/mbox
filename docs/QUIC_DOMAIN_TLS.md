# MBox QUIC with domain-verified TLS — staging notes

**Version context:** This is a proposed change against `v1.0.0-beta.4.1`, not a deployed release. The old IP-pinned profile remains accepted. Do not modify production UDP 7443 until a separate endpoint and Android APK pass physical testing.

## One source of truth for provisioning

`/etc/mbox/mbox.conf` can now contain:

```ini
[quic]
enabled = true
url = https://203.0.113.10:7444
server_name = node.example.com
ca_file =
```

`203.0.113.10` and `node.example.com` are **examples only**. `url` is the numeric UDP destination; `server_name` is the DNS identity verified by TLS and sent as SNI. This does not cause DNS lookup for the protected Android QUIC socket. `ca_file` is optional; when omitted, Go's available system root store is used. **Go's accessible trust roots on Android devices must be validated** — do not assume every device trusts the same issuers. An optional *stable public root CA* in `ca_file` can address a validated trust-store problem. Do not use an automatically renewed Caddy leaf as a trusted root.

To configure after installing a tested updated `mboxctl`, set `quic.url`, then `quic.server_name`, and enable `quic.enabled` last. The `[quic]` section is independent of `server.json`; setting these values does not require restarting the Noise server. Existing HTTPS-only profiles remain available when QUIC is disabled. The profile format is still v1.

`mboxctl user link alice`, `user app-link`, and `user qr` now automatically include operator-defined QUIC fields. Old explicit `--quic-url` with `--quic-ca-file` flags still work; use `--quic-server-name` for an explicit DNS identity. Providing any explicit QUIC override requires a complete endpoint + trust identity. The Android app stores these settings when a profile is imported. It does not read the VPS config itself.

**Limitations:** `mbox://` is currently a static imported profile. Later IP/domain changes require a new imported profile or a separately designed authenticated subscription update. This patch does not implement dynamic subscriptions or web-panel profile refresh.

## Android and native QUIC behavior

- Numeric IPv4 `quic_url` and Android protected physical UDP sockets remain mandatory.
- Optional `quic_server_name` enables certificate hostname verification against a DNS SAN, while the network socket still dials the numeric IP.
- Optional `quic_ca_pem` adds trust anchors, independently of `quic_server_name`.
- Legacy profiles without `quic_server_name` continue the previous IP SAN + PEM trust path.
- TLS verification is never disabled. Failure must fall back to HTTPS where configured, not silently use unverified QUIC.

## Caddy-managed certificate: operational design

The Caddy HTTPS certificate for `node.example.com` is authoritative and renewed by Caddy. **Do not** make `mbox-native-quic` read Caddy's key directory directly, and do not expose it through shared hosting.

The optional helper `tools/quic-caddy-sync.sh` takes environment variables `MBOX_CADDY_CERT`, `MBOX_CADDY_KEY`, `MBOX_QUIC_TLS_DOMAIN`, `MBOX_QUIC_TLS_STORE`, `MBOX_QUIC_UNIT`. Its modes are:

- `--check`: read-only certificate/key match, DNS identity and remaining validity check.
- `--stage`: copies validated material into `/etc/mbox/quic-caddy/current/` using a versioned directory and atomic pointer update; does NOT restart any service.
- `--sync`: on an explicitly wired QUIC systemd unit, replaces the pointer, restarts that unit, and rolls back the pointer on failed restart. An unchanged pair causes no restart.

**The helper, timer and Caddy-to-QUIC drop-in are NOT automatically installed or enabled.** The timer is only for a separately approved operator deployment after end-to-end tests. Its `systemctl is-active` check is not sufficient as end-to-end health proof.

An operator-installed systemd drop-in must keep existing `paths` and `hmac` credentials and replace **only** the sources of `cert` and `key`, e.g.:

```ini
[Service]
LoadCredential=
LoadCredential=paths:/etc/mbox/quic/paths.json
LoadCredential=hmac:/etc/mbox/origin-hmac.key
LoadCredential=cert:/etc/mbox/quic-caddy/current/server.crt
LoadCredential=key:/etc/mbox/quic-caddy/current/server.key
```

**Check the actual unit and path layout before installation.** The `LoadCredential=` reset removes earlier declarations, so omitted `hmac` or `paths` would break the service. The running unit's `ExecStart` should continue referencing `${CREDENTIALS_DIRECTORY}/...`. Caddy's certificate should remain on the server; never send its private key, debug credentials or `mbox://` secrets to support staff.

A supervised staging test must use a *different UDP port/service* from production. Test valid DNS name, mismatched SAN, missing chain, expired certificate, missing OS roots, mismatch cert/key, fallback and loss/reconnect. Compare Android beta.4.1 with the new candidate; old APK cannot use DNS-only QUIC TLS. Do **not** replace production 7443 certificate until affected clients have imported compatible profiles.

## Exit strategy / future domain migration

When replacing a domain, issue the new certificate, test a parallel endpoint and distribute new signed/authenticated profiles. The TCP/HTTPS frontend and QUIC gateway can have different hostnames on one registered domain. DNS TLD alone does not guarantee availability under network restrictions. Do not publish unsupported legal, tariff or censorship guarantees.
