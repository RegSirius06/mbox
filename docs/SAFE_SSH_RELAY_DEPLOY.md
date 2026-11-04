# Safe RC1 FastCGI deployment to shared hosting (SSH)

`mboxctl relay deploy` uses a staged server's actual `server.json` and its HMAC secret, **never** generates a new HMAC key and **never** silently deploys an old `/usr/local/lib/mbox/relay.fcgi` template. The caller must supply the verified relay ELF binary. The remote key, lock directory and rollback snapshots are kept outside the public webroot.

This command updates the FastCGI relay only. It does not modify Caddy, the origin daemon, `.htaccess`, index pages, or Android profiles (unless `--site` explicitly requests decoy page updates). Prepare and verify the origin and Caddy routing first. A successful HTTP health probe does not substitute for an end-to-end Android/Noise test.

## Prerequisites

- Password/SSH-key access from the origin/admin VPS to the shared-host account.
- Python 3 on the shared host.
- Existing webroot with a working FastCGI mapping.
- A **separate real directory** outside the webroot, owned by the shared-host account, mode `0700`. Example: `/var/www/USER/data/mbox-stage-private`.
- The RC1 origin running and its `server.json` referencing the correct HMAC secret.
- The *verified* RC1 FastCGI relay binary present on the admin VPS.
- `/etc/mbox/mbox.conf` identifying the same `base_path` and origin HTTPS URL used by the running frontend. The deployer fails closed if the staged origin uses a different `base_path`.

## First: dry run

```bash
sudo mboxctl relay deploy \
  --ssh USER@HOST \
  --remote-dir /var/www/USER/data/www/front.example.com \
  --remote-private-dir /var/www/USER/data/mbox-stage-private \
  --origin-config /etc/mbox-stage/server.json \
  --relay-binary /root/mbox-relay-compat.fcgi \
  --relay-sha256 EXPECTED_64_CHAR_HEX_SHA256 \
  --dry-run
```

`--dry-run` validates the local inputs without writing or contacting the shared host. It must print `DEPLOY PREFLIGHT OK`.

## Deploy over SSH

Remove only `--dry-run` from the command above. The new deployer:

1. Creates a compressed tar stream in memory and transfers the relay, config, and HMAC key over **one SSH session**.
2. Verifies the private directory belongs to the SSH user, has mode `0700`, and is outside the webroot. The webroot must already exist.
3. Verifies the existing site's origin target and API path prefixes match; refuses to overwrite a different site.
4. Copies existing files (including previous HMAC key) into a new private `backups/TIMESTAMP-ID/` directory, with a manifest.
5. Installs the key at `PRIVATE/origin-hmac.key` mode `0600`, the relay configuration under the webroot mode `0600`, the executable mode `0755`, and locks under `PRIVATE/locks` mode `0700`.
6. Verifies on-host SHA256, configuration and key contents. On an installation exception it restores the preceding files.
7. Requests `https://PUBLIC_URL/health` from the origin/admin host, requiring **HTTP 200** (body may be `ok` or `ready`). On failure, asks the remote host to roll back the exact backup ID. If the admin host cannot reliably reach the public URL, `--skip-health-check` is available **only after deliberate operator validation**.

For the first install, do **not** delete old webroot `.rollback-*` files until you have independently checked whether they are web-accessible. Subsequent secure deployments put rollback copies outside the webroot.

The deployment does not regenerate secrets or restart the origin. The FastCGI process manager may require recycling existing workers if it holds the previous executable open.

## Roll back

```bash
sudo mboxctl relay rollback \
  --ssh USER@HOST \
  --remote-dir /var/www/USER/data/www/front.example.com \
  --remote-private-dir /var/www/USER/data/mbox-stage-private \
  --backup-id latest
```

`--backup-id` also accepts a specific `BACKUP_ID` printed by the deployment command. Rollback restores the matching relay executable, relay configuration, and previous HMAC key. This affects the shared frontend only; coordinate Caddy routing separately if you changed the origin during the same maintenance window.

## Safety boundary

FTPS deployment is disabled in this HMAC-required release. Shared-hosting FTP users may be chrooted under their document root, so an FTPS script cannot reliably put the HMAC key in a directory that is not web-served. Use SSH or a separately audited provisioning mechanism instead.

`mboxctl relay bundle` continues to create a *local* diagnostic/manual bundle, not an automatically safe-to-extract webroot payload. Never upload its key or unedited `relay.json` into a public document root. The secure SSH `relay deploy` path builds a deployment-specific configuration and keeps the key outside the webroot.
