# REG.RU deployment-specific routing

`mboxctl relay bundle/deploy` manages the generated `relay.fcgi` and `relay.json`.

The exact `.htaccess`/FastCGI activation rule is provider/account specific and is intentionally not guessed here. Before publishing a reproducible deployment, copy the currently working live `.htaccess`, remove account/domain-specific data if needed, review it, and commit a sanitized `.htaccess.example`.

Do not replace a working shared-host `.htaccess` blindly: it may also contain unrelated website configuration.
