# QUIC TLS trust and certificate renewal — operator runbook

**Not installed automatically.** The beta.4 Android `quic_ca_pem` is already loaded by `crypto/x509` as a trust anchor and standard TLS chain, date and IP SAN verification is performed by quic-go. No InsecureSkipVerify is permitted. For a numeric IPv4 endpoint, the **leaf** certificate must contain an `IP:<endpoint>` SAN, not a DNS-only SAN. A leaf certificate copied to `quic_ca_pem` is effectively pinned: replacing it may require a one-time profile migration.

## Bootstrap a dedicated CA

On an **offline trusted machine**, create a long-lived P-256 CA (example: ~3 years):

```bash
umask 077
openssl req -new -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -days 1095 -sha256 -subj '/CN=MBox QUIC dedicated CA' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout ca.key -out ca.crt
```

The public `ca.crt` may be distributed in the `quic_ca_pem` profile field. Never put `ca.key` in Git, a user profile, APK, shared hosting or logs. Establish that the endpoint IP SAN will not change during leaf rotation.

Unattended issuance using `tools/quic-cert-renew.sh` **requires an online CA private key** in `/etc/mbox/quic/ca.key` (0600 root-only). A root compromise of the VPS would let the attacker issue new certificates until CA expiration. For stronger security keep the CA key offline and manually issue leaf certificates or use an external authenticated issuing service; then do not enable the unattended timer. **Choose the risk consciously.**

## Opt-in deployment on operator-controlled VPS

First back up current QUIC TLS files and service unit; validate the new CA on a test endpoint. On the VPS (with root access), install `ca.crt` and, only if accepting online issuer risk, `ca.key` in `/etc/mbox/quic/`. Confirm `ca.key` mode 0600, directory mode 0750 `root:mbox`. Install `tools/quic-cert-renew.sh` as `/usr/local/libexec/mbox-quic-cert-renew` mode 0755. Configure the QUIC service arguments to read a symlinked **pair** `/etc/mbox/quic/current/server.crt` and `/etc/mbox/quic/current/server.key`, rather than two independently updated files.

Create `/etc/mbox/quic/renew.env` (0640 root-owned) with public config:

```ini
MBOX_QUIC_ENDPOINT_IP=203.0.113.10
MBOX_QUIC_UNIT=mbox-native-quic.service
MBOX_QUIC_READ_GROUP=mbox
MBOX_QUIC_LEAF_DAYS=60
MBOX_QUIC_RENEW_BEFORE_DAYS=14
```

Replace the **documentation IP** `203.0.113.10` with your real numeric endpoint; never commit the actual endpoint if sensitive. Check the actual gateway systemd unit name. Install `deploy/systemd/mbox-quic-renew.service` and `.timer` into `/etc/systemd/system/`; `systemctl daemon-reload`. First run `systemctl start mbox-quic-renew.service` manually and verify QUIC **on an independent test client**, then optionally `systemctl enable --now mbox-quic-renew.timer`. The timer checks daily with randomized delay; it does not issue a cert when more than 14 days remain. Generation files and a pair of cert/key are validated before atomic `current` link swap; gateway restart failure rolls back the symlink and attempts to restart the old unit. Monitor `journalctl -u mbox-quic-renew.service` and TLS expiry proactively. A successful systemd restart alone does not prove end-to-end service health.

## Migration of old pinned leaf profiles

The old self-signed certificate that expires 2026-12-07 **cannot be silently replaced with an unrelated CA**. While the old authenticated HTTPS/Noise provisioning path still works, send an **operator-authenticated** new `mbox://` profile containing the same MBox identity and `quic_url` plus public **new CA certificate** in `quic_ca_pem`. The user explicitly reimports it once, tests QUIC to the new CA-issued leaf with matching IP SAN, and only then the old trust is retired. If changing certificates is too disruptive, use a separate QUIC test port/endpoint during transition; do not disable TLS checks. HTTPS-only profiles are unaffected.

The original profile URL and credentials are private. No runtime APK, Git commit, or support ticket should include them. No automated CA trust rollover has been implemented; this is an explicit operator step.

## Negative tests before release

Use an isolated QUIC staging gateway with (1) correct cert, (2) expired leaf, (3) foreign CA, (4) wrong IP SAN, (5) renewed leaf same CA, (6) failed issuance, (7) failed systemd restart/rollback, and (8) successful old-profile migration. Only (1) and (5) should authenticate. Do not mark any case passed without executing it.
