# MBox v1.0.0-beta.4.2 — prerelease

**Android 10+ (API 29), ARM64, `io.mbox.android`, `versionCode 9`.** This is a beta release, **not RC1**. The headline changes are a self-hosted web admin panel for routine MBox operations and persistent HTTPS subscriptions with multiple profiles.

## Headline feature: MBox web admin panel

Previously, routine administration required running `mboxctl` over SSH. The new web UI provides:

- individual and batch user creation, user listing and current-session visibility;
- enable/disable, renew or clear expiry, concurrent-session limits, PSK rotation, single and batch user deletion;
- direct `mbox://` profile retrieval, stable HTTPS subscription issuance, and revocation of a user's subscription links;
- creation, editing and deletion of HTTPS ingress endpoints used when rendering subscriptions;
- browser-based editing of `mbox.conf`, with validation, backup and apply/rollback attempts;
- common read-only diagnostics including configuration checks, resource limits, database integrity, users, counts and version.

The panel has separate authentication and is intended to be reached through a private administrative endpoint; it must not be exposed publicly without appropriate protections. **Routine administration no longer requires SSH or direct `mboxctl` use.** The CLI remains available for recovery and advanced tasks. **Not every `mboxctl` command is represented by a dedicated web UI action**: relay deployment, backup restoration and certain system-service actions still require CLI access. The web interface is not a complete one-to-one replacement for the entire CLI.

## HTTPS subscriptions and Android

- **Stable per-user HTTPS link.** Reopening “Show subscription” returns the same primary URL instead of issuing another token. Previously issued tokens remain active until revoked; their count is visible in the panel.
- **Dynamic provisioning.** The subscription service derives current `mbox://` profiles from operator configuration and the user database; ingress changes are reflected when clients refresh.
- **Multiple Android subscriptions.** Up to 12 independent subscriptions and 12 ingress profiles per subscription. Users can select, refresh individually or collectively, and remove local copies. Subscription URLs and cached profile credentials are protected with Android Keystore; a failed refresh preserves the cached configuration.
- **Existing direct `mbox://` imports remain supported.** A direct profile imported before upgrading continues to work. To select it again after switching to a subscription, a one-time re-import of its original link may be required.
- **Configurable public subscription path**, such as `/api/v2` rather than `/mbox/sub`. Changing the path invalidates old distributed URLs even when their bearer tokens are still valid; users need an updated URL.
- **Automatic QUIC→HTTPS transport selection remains.** Release builds prefer QUIC when provisioned, then fall back to HTTPS. This is not seamless flow migration or automatic switching between separate HTTPS ingress profiles during an active tunnel.

The tested deployment had **one** HTTPS ingress. Multi-ingress support exists in code, but real-world REG.RU+secondary-host failover and ingress selection were not yet field-tested.

## Compatibility with beta.4 / beta.4.1

**Partial backward compatibility is confirmed:** older direct `mbox://` profiles still connect via the tested **HTTPS/FastCGI** path (also used in network allow-list situations). This is not a universal censorship-bypass guarantee.

**QUIC from older direct profiles/links does not connect in the current deployment.** The latest QUIC/TLS configuration and certificate trust data differ from previously distributed profiles. A changed leaf certificate, with the old certificate still pinned in `transport.quic_ca_pem`, is a plausible explanation; no failed TLS transcript was captured to prove it is the only cause. This does **not** establish an incompatibility in the Noise/MBox wire protocol. QUIC requires a newly provisioned profile with the correct `quic_url` and trust material; do not disable TLS verification.

**HTTPS subscription URLs require a client with subscription support (beta.4.2 or later).** Older apps must continue using direct `mbox://` provisioning. Thus the tested HTTPS route remains backward-compatible, while older QUIC provisioning does not work with the current endpoint trust configuration.

## Observed test results

- Caddy and the subscription service serve the configured public route. Invalid tokens return `404` from MBoxSubscriptions.
- The web UI successfully changed `subscription.public_url` in `mbox.conf` and the matching Caddy route on the live VPS. Relevant services were active.
- The operator confirmed functional web administration and stable repeat access to the primary subscription URL.
- The production-signed Android APK (`versionCode 9`) upgraded the existing installed app using `adb install -r` without uninstalling it.
- A new HTTPS subscription imported and its profile connected. An older direct profile connected over HTTPS; its old QUIC configuration failed.
- The operator reported local tests passing after the patches. **Final GitHub CI, the exact source commit, checksum of the final published APK, and a complete final signed-build acceptance run must be recorded separately.**

Not yet demonstrated: two independent ingresses in one subscription under live network conditions; long soak; physical-interface IPv4/IPv6/DNS/TCP/UDP leak capture; Android Lockdown; controlled QUIC/HTTPS failures; fingerprint comparison; independent security audit.

## Known issues and limitations

- Android and web administration still need a usability redesign before RC1.
- QR image preview in the web panel may not render; copying the link works.
- No traffic accounting, per-user traffic quotas, or registered-device limits yet. Concurrent-session limits are **not** device limits.
- Revoking a subscription token blocks future provisioning from that URL, but **does not revoke PSKs already cached by clients**. Disable a user or rotate their PSK to stop VPN access.
- No seamless QUIC/HTTPS handover or automatic selection of a different HTTPS ingress within a running tunnel.
- Complete per-parameter `mbox.conf` documentation and dedicated UI support for advanced maintenance tasks are unfinished.
- QUIC TLS trust migration and ongoing certificate renewal require separate operational validation.

## Production readiness

**Operational status:** beta.4.2 works in the maintainer's live private deployment, with the tested HTTPS path functional. **Release status:** suitable for a *GitHub prerelease* once the reviewed commit, successful CI, production-signed APK verification and final smoke tests are recorded. **General production/RC1 readiness is not established** without the outstanding reliability, leak and security tests.

## Release artifacts

- `MBox-1.0.0-beta.4.2-android-arm64.apk` signed with the **existing** production key.
- `MBox-1.0.0-beta.4.2-android-arm64.apk.sha256` for the **exact uploaded** file.
- Git tag `v1.0.0-beta.4.2` on the reviewed commit with matching passing CI.
- Verify the APK signing certificate SHA-256 against the existing trusted pin with `tools/check-production-apk.sh`.

Never publish direct `mbox://` profiles, subscription tokens, PSKs, private keys, SQLite user databases or raw sensitive logs.
