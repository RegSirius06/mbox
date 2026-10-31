# MBox 1.0.0-beta.1

First beta release of MBox.

MBox is a self-hosted encrypted multiplexed transport designed to operate through non-streaming HTTP intermediaries.

## Highlights

- encrypted multiplexed TCP transport
- per-device authentication and provisioning
- retry, deduplication, buffering and frame reordering
- Android VpnService client with automatic reconnect
- SOCKS integration and downstream proxy routing
- SQLite-backed user management with `mboxctl`
- REG.RU FastCGI relay generation and deployment tooling
- optional server-originated cover traffic
- installer, migration, backup and recovery tooling

## Android

The attached APK is a debug-signed beta build:

`MBox-1.0.0-beta.1-debug.apk`

It is intended for testing and is not yet a production-signed Android release.

## Known limitations

- TCP tunnelling only; native UDP/QUIC tunnelling is not supported.
- Cryptographic design has not received an independent security audit.
- Static server X25519 authentication does not provide strong post-compromise forward secrecy.
- Graceful session CLOSE is best-effort; idle expiry remains the authoritative cleanup mechanism.
- Cover traffic affects the shared frontend activity profile but does not conceal client last-mile traffic.
- Extended soak, fuzz and heavy multi-device testing are still pending.

## License

MBox is licensed under `AGPL-3.0-or-later`.

Third-party components retain their respective licenses.
