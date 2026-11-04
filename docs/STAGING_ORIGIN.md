# Isolated origin staging (RC1 hardening)

The production server currently listens on `127.0.0.1:18449`. For a staging
origin on `127.0.0.1:18450`, keep independent configuration, credentials,
admin socket, cover-status path and runtime-status path. The server will exit
on an invalid `MBOX_RUNTIME_STATUS` override rather than modifying production
state at the default `/run/mbox/status.json`.

A staging unit should set `Environment=MBOX_RUNTIME_STATUS=/run/mbox/stage-status.json`
and use an independent `crypto.credential_reload_socket`, distinct
`cover.status_file` and a copied credential directory. It must not run
`mboxctl sync` against the live production daemon.

The origin enforces HMAC for all HTTP handlers, including `/health/ready` and
`/metrics`. A bare unsigned `curl` will return camouflage 404 even for a healthy
origin. Exercise readiness using a correctly signed request and a distinct
staging HMAC key.

A safe REG.RU setup stores `origin-hmac.key` **outside** the HTTPS document
root and uses an absolute `origin_hmac_secret_file` in `relay.json`.
The current `mboxctl relay deploy` uploads the executable and configuration,
**but not the HMAC secret**. Provision and verify that secret independently.
Never assume mode 0600 prevents an HTTP download if the CGI and static server
share a Unix user. Confirm direct HTTPS requests to sensitive config/secret
paths are denied, without revealing response bodies in logs.
