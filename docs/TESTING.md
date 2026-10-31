# Testing status

## Release validation for 1.0.0-beta.1

- `gofmt`, `go vet`, and Linux builds for server/client.
- ARM64 Android cross-build of the shared Go client core with `CGO_ENABLED=0`.
- FastCGI relay build.
- Python syntax compilation for `mboxctl`.
- shell syntax checks for installer/build scripts.
- fresh `mboxctl init` in an isolated filesystem root.
- SQLite single and bulk user creation.
- disable/revoke/rotate/rename runtime rendering.
- provisioning link generation and redacted decode.
- REG.RU bundle generation.
- SQLite-consistent backup creation/verification.
- local end-to-end MBox v2 session handshake using a user created in SQLite.
- live server `SIGHUP` auth-map reload followed by a successful handshake from a newly added user, without restarting the server process.
- legacy JSON + per-user PSK-file migration into SQLite/runtime JSON.

## Field behaviour already exercised during development

The Android client has been exercised end-to-end through a real buffering shared-hosting FastCGI frontend into an RU origin and a SOCKS next hop. Telegram/TCP application traffic was observed working through that path.

The Android supervisor has also been tested against transport loss: it performs bounded reconnect backoff rather than an infinite tight retry loop.

## Known areas needing more testing

- sustained multi-hour/multi-day Android operation;
- Android sleep/doze and vendor-specific process management;
- Wi-Fi ↔ mobile network transitions under load;
- multiple simultaneous clients/users;
- high shared-host relay contention;
- large file throughput and memory bounds;
- malformed/adversarial protocol inputs;
- fuzzing of MUX/record/envelope parsers;
- cryptographic review;
- occasional shared-host HTTP 5xx responses observed during development.
