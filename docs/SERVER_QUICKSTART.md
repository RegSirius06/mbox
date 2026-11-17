# MBox 1.0.0-beta.4.3: first-time server setup

[Русский](ru/SERVER_QUICKSTART.md) · [Android user guide](GETTING_STARTED.md) · [Admin reference](ADMIN.md)

This is a starting point for someone who owns a Linux VPS. Prior knowledge of FastCGI, Noise or HMAC is not required.

## Requirements

- Linux VPS with **root SSH** administration access. The *origin* receives MBox VPN sessions and may forward to a separate Internet exit.
- MBox source files, system dependencies, and Go toolchain (checked by the installer). QUIC/HTTPS requires appropriate network, domain, port, and TLS configuration.
- Optionally, a **separate shared hosting** account supporting FastCGI (for example REG.RU) as a public HTTPS ingress. It does not need to reside on the origin VPS.
- A verified backup if this is not a fresh server.

## Components

```text
Android --QUIC/UDP-------------------------> origin VPS -> Internet / exit
        +--HTTPS--> shared hosting FastCGI -> origin VPS ---+
Operator --SSH tunnel--> private admin Unix socket on VPS
```

Shared hosting is not automatically your Internet exit. Beginners should bring up the origin and test a profile before introducing the optional HTTPS relay.

## Step 1. Check prerequisites

From a copy of the repository **on the VPS**:

```bash
./install.sh --check
```

This is a read-only check. Fix reported missing requirements before installing.

## Step 2. Install a fresh origin

Run `./install.sh` from an **interactive terminal** and choose **1) New server installation**. This invokes `--fresh-install` and can offer an optional REG.RU setup wizard afterward. Without an interactive terminal, running it with no arguments remains a safe no-op. To skip the menu, explicitly run `./install.sh --fresh-install`.

**Do not** use `--fresh-install` on an existing MBox installation. The installer refuses an existing config.

## Step 3. Verify the origin

```bash
mboxctl version
mboxctl doctor
mboxctl resources
systemctl is-active mbox-server.service
```

Back up configuration, server secrets and SQLite user database securely. Never publish these in bug reports or screenshots.

## Step 4. Web administration

Install separately with `./install.sh --with-admin` if needed. The recommended access path is a local browser over an **SSH tunnel**:

```bash
ssh -N -L 127.0.0.1:8765:/run/mbox-admin/admin.sock root@YOUR_VPS
```

Open `http://127.0.0.1:8765/` locally. A public `https://panel.example.com` is an **opt-in** Caddy reverse-proxy configuration with Host/Origin allowlisting, protected cookies and additional access control; see [the public-panel guide](MBOX_ADMIN_PUBLIC_ACCESS.md). Do not expose the unprotected admin HTTP interface directly.

## Step 5. Provision a user

Create a user and obtain a `mbox://` profile or HTTPS subscription from the panel. Both are bearer secrets. On the Android device, import the link and connect according to [the user guide](GETTING_STARTED.md).

## Step 6 (optional). FastCGI shared-hosting relay

The first-install wizard may offer SSH deployment of a REG.RU/shared-hosting frontend. It asks for a `user@host`, an absolute webroot path, a **private directory outside webroot**, the origin `server.json`, and the built `relay.fcgi`. It runs `mboxctl relay deploy --dry-run`, then requires an explicit `DEPLOY` confirmation before transfer.

It does **not** buy hosting, connect to a REG.RU account API, or store SSH passwords. See [shared hosting](REG_RU_SHARED_HOSTING.md) and [wizard details](BETA4_3_INSTALL_WIZARD.md).

## Updating an existing origin

`--check` is read-only, `--with-admin` updates the admin UI/CLI, and `--with-subscriptions` updates subscriptions/admin. **`--upgrade-origin` replaces origin binaries and restarts `mbox-server.service`**; active clients can disconnect. Perform it only after a verified backup and an explicit maintenance decision. Updating the panel HTML alone does not update the origin version; confirm with `mboxctl version`.
