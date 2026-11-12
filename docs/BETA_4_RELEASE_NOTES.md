# MBox 1.0.0-beta.4 — release notes (DRAFT)

[Русский](ru/BETA_4_RELEASE_NOTES.md)

> **Draft, not a published release.** Update this file only after building, signing, checking, and testing the release APK. The latest published beta may still be beta 3.

## Headline changes

- **Application UDP support:** UDP flows are now forwarded through MBox, including over the existing HTTPS/TCP route for restrictive access networks. Controlled Android STUN diagnostics were used to validate UDP transport in the development build.
- **Direct native QUIC / HTTP/3 transport:** UDP-based connection to the MBox VPS gateway, bypassing the shared-hosting relay. Uses HTTP/3 streams, **not** QUIC DATAGRAM.
- **Android Auto transport selection:** retries over HTTPS when QUIC fails; transport preference is reevaluated when the underlying network changes. This is connection-level failover, not multipath bonding.
- **Gateway hardening:** native QUIC runs under an unprivileged systemd DynamicUser with `NoNewPrivileges`; observed `systemd-analyze security` score improved from **9.6 UNSAFE** to **3.8 OK**. The score is a static sandbox exposure heuristic, not a security certification.
- **Diagnostics:** explicit QUIC connection stages and transport selection logs aid investigation of carrier- and hosting-specific failures.

## Field validation to date

Manual HTTPS and QUIC, Wi-Fi/mobile Auto switching, and QUIC + Noise handshake have succeeded on the maintainer's Android device. The maintainer also reported one successful MegaFon restricted-network test. Local tests: Go test/race/vet and **96 Python tests** passed; debug Android APK built. These observations **do not** establish that the release APK works, or that all carriers are supported.

## Limitations / known issues

- The November 12 shared-hosting FastCGI HTTP 500 incident was cleared by restarting account-owned workers. Root cause is unknown. Continued field reports are needed.
- Leak-free operation on every physical interface, fail-closed behavior under all Android lifecycle failures, and extensive multi-user long-term reliability have **not been independently verified**.
- No universal DPI resistance claim; Chromium-derived HTTPS is not equivalent to an exact browser fingerprint.
- No QUIC DATAGRAM, simultaneous multipath, transparent migration of live application flows or desktop clients.
- Backward binary compatibility with prior experimental betas is **not promised**.
- The deployed lab QUIC certificate must be renewed or replaced before expiry (reported **2026-12-07**), with a controlled trust update.

## Testers and contributors

Please open a [GitHub Issue](https://github.com/RegSirius06/mbox/issues) for reproducible defects or a Pull Request for code improvements. Include sanitized logs, device/OS version, network/operator, transport mode, approximate time with time zone and reproduction steps. Never share provisioning links, PSKs or private keys. See [field test guidance](BETA_4_FIELD_TESTING.md) and [contributing](../CONTRIBUTING.md).

## Release-gate checklist (must be filled before publication)

- [ ] `VERSION` and Android `versionName/versionCode` set to beta 4.
- [ ] Native QUIC binary present in the **release** APK (not only `src/debug`); QUIC endpoint and public trust configured per deployment rather than silently hardcoded from a private debug setup.
- [ ] Expected future signing certificate SHA-256 is `17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586`; compare it to the certificate in the **final APK** before publishing (this is not a claim the APK is signed yet).
- [ ] Signed release APK uses maintainer-controlled key; `apksigner verify` and pinned certificate SHA-256 pass.
- [ ] Real installed release APK tested on physical Android for HTTPS, direct QUIC, Auto, TCP and UDP.
- [ ] Fail-closed/Lockdown test completed or limitation explicitly documented in final release announcement.
- [ ] No private key, PSK, credentials or deploy-specific secrets added to the public APK/repository.
- [ ] Changelog/README/Release notes match the published artifact; attach APK SHA-256 to GitHub prerelease.
