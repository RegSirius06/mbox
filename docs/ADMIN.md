# Administrator notes

The canonical user-facing command reference is in the root `README.md` and `mboxctl --help`.

Important state locations:

```text
/etc/mbox/mbox.conf                 human configuration
/etc/mbox/server.json               generated runtime config
/etc/mbox/server.key                secret server X25519 private key
/etc/mbox/server.pub                server public key
/var/lib/mbox/users.db              authoritative users/PSKs
/var/lib/mbox/regru/current/        generated REG.RU files
/var/lib/mbox/origin/current/       generated origin proxy snippet
```

Never hand-edit `server.json`; edit `mbox.conf` through `mboxctl config set` or carefully by hand followed by `mboxctl config validate && mboxctl sync --restart`.


## Cover traffic

The feature is disabled by default. Existing pre-beta configurations are upgraded by `install.sh`; the new `[cover]` section is materialized automatically.

Recommended activation sequence:

```bash
mboxctl cover simulate --hours 24
mboxctl cover test
mboxctl cover enable
mboxctl cover status
```

Disable without editing JSON:

```bash
mboxctl cover disable
```

Useful tuning examples:

```bash
mboxctl config set cover.max_rps 4
mboxctl config set cover.max_requests_per_hour 1200
mboxctl config set cover.idle_after_ms 120000
mboxctl config set cover.quiet_weight 35
mboxctl config set cover.casual_weight 40
mboxctl config set cover.active_weight 20
mboxctl config set cover.burst_weight 5
```

`cover test` makes one real GET through the public frontend to the server cover handler. `cover simulate` performs no network I/O.
