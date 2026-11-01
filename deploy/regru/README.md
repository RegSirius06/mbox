# REG.RU shared-hosting deployment

The REG.RU/shared-hosting frontend is a separate component from the MBox origin VPS and from the eventual Internet exit.

A typical layout is:

```text
client -> HTTPS -> REG.RU shared-host site/FastCGI -> HTTPS -> MBox origin -> sing-box -> chosen exit
```

The origin can be RU/FI/PL/etc.; sing-box can route onward to another server. Do not install the decoy website on the origin merely because the origin currently happens to be an RU VPS.

`mboxctl relay bundle/deploy` manages generated `relay.fcgi` and `relay.json`. The bundle also contains:

```text
site/index.html
site/404.html
```

These are the current `Private Rooms` decoy pages. A normal relay deploy deliberately leaves an existing web site untouched. Add `--site` only when you intentionally want MBox to publish these pages into the chosen shared-host web root:

```bash
mboxctl relay deploy \
  --ssh USER@HOST \
  --remote-dir /path/to/webroot \
  --site
```

Existing site files receive timestamped rollback names before replacement.

The exact `.htaccess`/FastCGI activation rule is provider/account specific and is intentionally not guessed here. Preserve the currently working FastCGI mapping. Before publishing a generic `.htaccess` example, sanitize account/domain-specific values and verify it on a disposable/shared-host test path.

Do not replace a working shared-host `.htaccess` blindly: it may contain unrelated website configuration.
