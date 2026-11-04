# Additional MBox hardening — 2026-11-04

This patch series supplements beta.2.1 and the separately prepared P0/P1 hardening series. It is **not** a complete production-security sign-off.

## Android/Go log confidentiality

- The standard Go logger now removes entire `http://`, `https://` and `mbox://` URLs before output. This matters because `net/http` wraps network errors with full request URLs and MBox frontend paths may contain secret deployment tokens.
- PSK/password/token/private-key fields, Basic/Bearer credentials, line breaks, and oversized diagnostic lines are redacted or bounded.
- Android core-reader, Cronet exception and reconnect-status messages are also sanitized.
- Unit/Java regression tests include wrapped URLs, bearer/basic authorization, key-value secrets, newline injection, and line limits.

**Limits:** This covers the standard Go logger and selected Android logcat paths, not arbitrary calls to `fmt.Print`, third-party libraries or native code. It does not remove historical logs already present on a device/server. Never publish full `mbox://` profiles, private keys, raw URLs or backups.

## Android Go core / broken logging pipe

The Android supervisor previously observed process exit code 141 after handovers. Code 141 corresponds to `SIGPIPE` termination. Go ordinarily terminates when writing to a disconnected standard output/error pipe. The client now ignores `SIGPIPE`, so a broken pipe produces `EPIPE` instead. A subprocess regression test closes its stdout deliberately and checks survival.

This **does not prove** the original 141 reports were caused solely by the log pipe. Real Wi-Fi/mobile/airplane handover tests are still mandatory. The native code must still handle transport errors, not reconnect blindly.

## Production APK release gate

`tools/check-production-apk.sh` requires a non-empty APK, a valid `apksigner` verification result, a non-debug signer DN, package ID `io.mbox.android`, and a non-debuggable manifest. It prints SHA-256. The tool deliberately refuses unsigned and debug-signed APKs.

Usage (on an Android SDK machine):

```bash
bash tools/check-production-apk.sh android/app/build/outputs/apk/release/app-release.apk
```

**Limits:** This is a pre-release gate, not a signing tool and not a complete supply-chain provenance system. It does not guarantee a trusted signing identity; production releases must verify an expected pinned certificate fingerprint in release infrastructure. Debug builds remain suitable for local development only.

## Remaining critical work

- Native authenticated UDP datagrams, four-point packet-capture tests, and real Android lockdown/`VpnService.protect()` acceptance.
- Enrollment token flow + profile signatures + client-generated asymmetric credentials.
- Reproducible native dependencies, signed release provenance, SBOM, Gradle dependency verification.
- Load/soak/FD/goroutine/heap/fingerprint test campaigns.

## Deployment order

1. Keep the current server installation intact and save its rollback archive.
2. Apply the earlier P0/P1 series to a **clean local source branch**, run its tests, and resolve any conflicts.
3. Apply this additional series and rebuild Android on the Arch machine.
4. Run a real-device Android network-switch/leak acceptance test **before** any production rollout.
5. Stage server-side trust and credential changes along with the matching REG.RU relay configuration; do not update only one side of an HMAC trust boundary.
6. Update the RU origin only after a tested rollback path and matched client/relay artifacts are ready.
