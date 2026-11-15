# MBox subscriptions — beta.4.2

The permanent `https://DOMAIN/<configured-path>/<token>` URL returns JSON v1 containing standard `mbox://` profiles for one or more HTTPS ingress endpoints. Tokens are 256-bit bearer secrets. Only SHA-256 digests are stored in SQLite. The canonical token is re-derived with a dedicated 32-byte server-only key and per-user random nonce; legacy issued tokens remain valid but their URLs cannot be recovered. Revoking a token, disabling/deleting a user, or expiration prevents any further profile retrieval. The active MBox Noise key, PSK, QUIC settings and camouflage parameters come from the existing `mboxctl` link generator. All ingress variants share the same credentials but have different `transport.url` fields.

Two relay sites such as REG.RU and Beget are independent *ingress* paths, not independent origin servers. Both must forward to the same origin and support the same camouflage paths and forwarding protocol. Beget compatibility needs separate real-world testing.

## Stable subscriptions and revocation

The **Show subscription** button is idempotent: it issues one canonical subscription per user (if missing), then returns exactly the same token on every later click. The displayed URL base is dynamically read from `[subscription] public_url`, so changing the public route updates displayed links for everyone but invalidates previously shared URLs at the old path. Existing historical links, which were issued using the old random-token algorithm, continue to work without reissue. Their plaintext cannot be recovered from the stored SHA-256 digests. The UI displays the count of active links, including legacy ones.

**Revoke all subscriptions** invalidates every token for this user and removes the canonical nonce. A subsequent click creates a new canonical link. Database-level `AFTER DELETE` trigger revokes all subscriptions when a user is removed even directly through `mboxctl`; reusing the same ID does not resurrect past URLs.

The dedicated secret key `/etc/mbox/subscription-link.key` is created with mode `0600` on the first canonical subscription. **Back it up alongside `users.db`**. `mboxctl backup create` includes `/etc/mbox`; losing this key makes existing canonical URLs impossible to redisplay (although public GET still works by digest). The system fails closed instead of silently rotating the key. Never publish the key or token-bearing URLs in logs, Git, screenshots or support conversations.

## Deployment

Run local tests and apply the patch on your Arch development checkout first. Transfer updated sources to the existing VPS. Run `./install.sh --check` and `./install.sh --with-subscriptions`. This enables a loopback-only server on `127.0.0.1:18450` and updates the SSH-only admin UI; it does **not** touch running origin, QUIC, or Caddy.

Run `python3 tools/mbox-subscription/caddy_route.py --check` then `--apply` for a simple single-domain Caddyfile with rollback. Otherwise, explicitly add this route to your existing HTTPS Caddy virtual host (Caddyfile example; never overwrite existing Caddy configuration):

```caddyfile
handle /mbox/sub/* {
    log_skip
    reverse_proxy 127.0.0.1:18450
}
```

Do **not** use `handle_path`: the subscription server requires the original `/mbox/sub/` prefix. Protect bearer URLs from Caddy access logs, reverse-proxy debug dumps and analytics. Validate Caddy configuration before reloading it. Use the equivalent route if using Caddy JSON/API. Test `curl -i https://DOMAIN/mbox/sub/invalid`: it must return 404.

Default subscription URL base is derived from `[quic] server_name` (preferred) or `[server] public_url`; override with `[subscription] public_url = https://DOMAIN/mbox/sub` in `mbox.conf`. An endpoint JSON file (`/etc/mbox/subscription-endpoints.json`, mode 0600) holds up to 12 ingress URL variants. The admin UI can display/revoke links, add or remove ingress URLs, and delete multiple users with explicit confirmation. The existing `mbox://` direct import remains supported on Android. HTTPS subscriptions are encrypted at rest with Android Keystore, and the client can manually choose a downloaded ingress variant. Refresh occurs when the app resumes while VPN is stopped; it does not bypass VPN fail-closed behavior.

## Explicitly not yet implemented

Persistent cryptographically bound device registration limits, per-user traffic counters, dedicated forms for every advanced mboxctl command, and automatic active-tunnel HTTPS ingress failover. Existing per-session and expiration limits stay as implemented by origin; do not call concurrent-session limits registered-device limits. Additional origin protocol and security regression tests are necessary before these capabilities are safe to deploy.

## Browser admin and emergency CLI

The SSH-only admin panel provides forms and read-only diagnostics. For emergency operation, `mboxctl` remains available via SSH. QR PNG is generated on the VPS by `qrencode`. No arbitrary shell or full `mboxctl` console is exposed through the browser.
