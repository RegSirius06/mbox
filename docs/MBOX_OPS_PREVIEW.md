# MBox beta.4.1 — operations preview

**Status:** experimental implementation for a separate nonproduction branch, based on `0191357bf8e2c94b8aca397460aa0f32218aabc7`. Not a published RC1 or a production-ready installer.

This preview adds a safer staged `install.sh`, explicit QUIC gateway opt-in, installed-but-disabled TLS renewal tooling, and an SSH-only MBox Admin interface. Existing SQLite accounts remain authoritative. UI mutations use the existing `mboxctl` command whitelist. The admin Unix socket is root-owned mode 0600; connect through an SSH local-to-remote-unix-socket forward, not a public web server. An admin password (scrypt), temporary sessions and CSRF validation are required.

To inspect on a **test VPS** after applying the overlay:

```
./install.sh --check
./install.sh --with-admin
ssh -N -L 127.0.0.1:8765:/run/mbox-admin/admin.sock root@TEST_VPS
```

Open `http://127.0.0.1:8765` on the operator's machine. The SSH command runs on the operator's machine. TLS is provided by SSH. Do not expose that local forwarding port to other devices.

Current UI links are per-user **static** `mbox://` profiles containing credentials, not dynamic subscription URLs. A real subscriber-facing endpoint must be designed separately with revocable download tokens, authenticated rotation and anti-abuse controls; it must never reuse the root admin socket. Optional QUIC metadata for links can be specified in root-owned `/etc/mbox-admin/admin.env`:

```
MBOX_ADMIN_QUIC_URL=https://203.0.113.10:7443
MBOX_ADMIN_QUIC_CA_FILE=/etc/mbox/quic/ca.crt
```

Without it the UI generates HTTPS-only profiles. Example IP above is not a real deployment address. Do not turn on unattended leaf rotation before existing clients have migrated from the old pinned self-signed leaf to a validated CA.

The preview lacks: production VPS and REG.RU integration tests, full clean-install acceptance, automated installer rollback, traffic accounting, IP limits, subscription URLs, physical leak tests and long soak. Do not deploy it to production solely because its local tests pass. See `docs/ru/MBOX_OPS_PREVIEW.md` for the diagnostic commands and operational details in Russian.


## Safe installer modes (beta.4.2 patch)

`./install.sh` without arguments performs **no operations**. `--check` is read-only. Actions are independent and explicit:

- `--with-admin` installs/updates the root-only Unix-socket web UI and `mboxctl` without restarting origin, either QUIC gateway, Caddy or REG.RU. An initial admin password prompt is required; access the socket through SSH as root.
- `--upgrade-origin` explicitly updates and **restarts** the MBox origin, potentially interrupting active sessions. Backups are saved in `/root/mbox-preinstall-*`; rollback remains a manual operator action, and live SQLite must never be overwritten blindly.
- `--fresh-install` is allowed only without an existing MBox configuration.
- `--with-quic` currently **refuses** to operate: production UDP 7443 and staging UDP 7444 must keep their existing dedicated `LoadCredential` services. Use the separate operator-controlled Caddy sync tooling for certificate deployment.

The admin UI is still a preview and requires a separate acceptance test before production dependence.
