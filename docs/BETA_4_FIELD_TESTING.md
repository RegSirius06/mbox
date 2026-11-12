# MBox beta 4 — field testing, diagnostics, and known risks

[Русский](ru/BETA_4_FIELD_TESTING.md)

## Why real users matter

Some failures depend on carrier allow-lists, mobile radio transitions, shared hosting policies and FastCGI process lifetime, intermediary buffering, NAT, individual Android builds and regional routing. A local simulator cannot reproduce every combination. Wider opt-in testing improves the chance of discovering failures; **it does not guarantee that every bug will be found or that a VPN is leak-free**.

## Confirmed observations as of 2026-11-13

- Android manual HTTPS and direct native QUIC were used successfully in field tests.
- Android Auto switched from QUIC on Wi-Fi to HTTPS after connection failure on a restricted mobile network, and back to QUIC when the underlying network changed.
- The maintainer reported successful HTTPS connectivity on a MegaFon restricted mobile connection. It is an **individual observation**, not a universal carrier compatibility claim.
- A native QUIC Android connection completed QUIC handshake, Noise handshake and tunnel establishment after gateway privilege isolation was enabled.
- The maintainer's local test run reported Go tests/race/vet passing, **96 Python tests passing**, source-layout validation and a successful **debug APK** build. Release APK equivalence is **not established**.

## Known unexplained incident: FastCGI HTTP 500 (2026-11-12)

The shared-hosting HTTPS frontend intermittently returned HTTP 500; Apache `mod_fcgid` reported connection resets and missing FastCGI response headers. A standalone FastCGI health request succeeded. Restarting the account-owned `relay.fcgi` workers restored the route. **Root cause remains unknown**. Long uptime, cover, request handling and hosting behavior remain hypotheses, not confirmed causes. No automatic restart loop or recovery guarantee should be assumed. A later `mboxctl relay test` returning 404 is not, by itself, proof of relay malfunction: an ordinary unmatched web path may be intentionally concealed.

## Recommended acceptance checks

1. HTTPS and QUIC: start a new Noise session; test TCP traffic.
2. UDP through HTTPS: run the controlled STUN test and confirm the result traverses the VPN.
3. Auto: verify fallback on initial QUIC failure and after an established QUIC connection is lost.
4. Fail-closed: enable Android Always-on VPN + **Block connections without VPN**; terminate the application and test new connections. Do not equate an app-level test with a physical packet-capture audit.
5. Record Android versions, carrier, Wi-Fi/LTE/5G, roaming, transport mode, time zone, approximate time, MBox build and **redacted** diagnostic stages.
6. On any unexplained HTTP 500, note the frontend response code, relay worker state and **new** Apache error-log entries; do not expose wrapper URLs or credentials.

## Report a bug

Use [GitHub Issues](https://github.com/RegSirius06/mbox/issues) and select the **Bug report** template. Search existing issues first. State reproducible steps, expected and actual behavior, the network and selected transport, whether the VPN used Android Lockdown, and a timestamp with time zone.

Do **not** post `mbox://` provisioning URLs, passwords, PSKs, HMAC secrets, private keys, certificate private material, unredacted network paths, unredacted logcat, user databases, or real user traffic. For suspected vulnerabilities, do not post an exploit or private secrets in a public Issue; check whether the repository offers a private security-advisory reporting channel.

Code contributions are welcome through fork → branch → Pull Request, subject to review. See [CONTRIBUTING.md](../CONTRIBUTING.md).
