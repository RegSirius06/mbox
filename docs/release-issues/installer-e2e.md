# Staging acceptance: fresh install, explicit upgrades and rollback (beta.4.4)

**Severity:** high operational validation gap (P1). **Component:** install.sh, systemd, origin/gateway.

`bash install.sh --check` and shell syntax/tests passed on the development machine, but a read-only check does not prove real installation/upgrade. On a fresh disposable VPS, test interactive and explicit fresh-install; on a second staged VPS with existing origin, test `--upgrade-origin`, preserving custom systemd units, DATAGRAM socket ACL, existing native QUIC gateway, subscription DB, signing keys and relay credentials. Simulate a failed origin startup and verify actual rollback (including socket/ACL) rather than only checking shell syntax. Confirm commands do not reach REG.RU or overwrite current production without explicit consent. Record logs with secrets redacted.
