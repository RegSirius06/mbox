# MBox Admin: SSH access and optional HTTPS hostname

**Default:** root-owned Unix socket `/run/mbox-admin/admin.sock` inside a `0700` runtime directory, with socket mode `0600`. SSH port forwarding remains the recommended mode. Applying this patch does **not** enable public access, alter firewall rules or change production services.

**Opt-in:** `https://panel.example.com` via Caddy (TLS), separate Basic Auth, and the existing MBox Admin login. The Python backend still listens **only** on a Unix socket. An allowlisted HTTPS `Host`, matching `Origin` on public POST requests, and `Secure; HttpOnly; SameSite=Strict` session cookies are enforced. Forwarded headers do not grant trust.

## Enable explicitly

1. Back up and install the changed `mbox_admin.py` and `index.html` separately from origin, then restart **only** `mbox-admin.service` (Python source reload is required). Do not restart origin/QUIC/REG.RU.
2. Confirm Caddy's service account and primary group: `getent passwd caddy`, `getent group caddy`. Modify group configuration if your distribution differs.
3. Set up DNS for `panel.example.com`, ensure Caddy can obtain a TLS certificate, and keep Python off any public TCP listener.
4. Copy `deploy/systemd/mbox-admin-public.conf.example` to `/etc/systemd/system/mbox-admin.service.d/public.conf` after replacing the hostname. Do not overwrite `/etc/mbox-admin/admin.env`.
5. `systemctl daemon-reload && systemctl restart mbox-admin.service`. Check runtime directory is `750 root:caddy` and socket is `660 root:caddy` using `stat -c '%a %U:%G %n' /run/mbox-admin /run/mbox-admin/admin.sock`.
6. Generate a **different** Caddy Basic Auth password with `caddy hash-password`; use the hash and your real hostname in `deploy/caddy/mbox-admin-public.caddy.example`. Add the site block to your existing Caddyfile without replacing any VPN routes. Before Caddy 2.8, the `basic_auth` directive is `basicauth`.
7. Back up your Caddyfile, run `caddy validate --config /etc/caddy/Caddyfile`, and only after validation `caddy reload --config /etc/caddy/Caddyfile`. Reloading Caddy can affect existing connections; schedule carefully.
8. Verify valid HTTPS, Basic Auth + MBox login, CSRF, rejection of wrong Host/Origin, `Secure` cookie, and preservation of localhost SSH access, without altering production users.

## Disable and roll back

Remove only the public-mode systemd drop-in and Caddy site block; validate/reload Caddy, then `systemctl daemon-reload && systemctl restart mbox-admin.service`. Private socket mode returns to `0700/0600` and SSH access remains possible.

**Security:** Caddy HTTPS and an independent authentication barrier are required. Prefer an IP allowlist or private overlay network when possible. MBox Admin can change root-owned service configuration; it is not designed as an open public website. Do not reveal credentials, provisioning links, or operator secrets. The public host cannot be enabled from UI alone; it is a server-side explicit choice.
