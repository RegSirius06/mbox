# MBox beta.4.1 — reproducible tests and evidence

[Русский](ru/TESTING.md) · [Leak matrix](ANDROID_FAIL_CLOSED_TEST_PLAN.md) · [Acceptance](BETA4_1_STATUS.md)

## Automatic checks (from exact commit)

```bash
cd ~/src/mbox-main
git diff --check
GO111MODULE=off go test -race ./client ./server
GO111MODULE=off go vet ./client ./server
(cd native-quic && go vet ./... && go test -race -count=1 ./...)
python3 -m pytest -q tests
python3 tools/check-source-layout.py
./tools/secret-scan.sh
(cd android && ./tools/build-debug.sh && ./gradlew assembleRelease)
python3 tools/check-unsigned-apk.py android/app/build/outputs/apk/release/app-release-unsigned.apk
```

A **green final run** must be recorded with commit SHA and test counts. In the received initial log: Go client/server and native QUIC passed, Python was 114 passed / 1 failed due to a format-sensitive assertion; an assertion fix was supplied but an all-green rerun has not been independently recorded in this conversation.

## Final production-signed APK

Use **the existing keystore**, not a debug or new signing key, with `tools/build-release-apk.sh` and verify `tools/check-production-apk.sh`. Check `versionCode=8`, non-debuggable, application ID, both ARM64 native executables, and absence of lab assets. Calculate a **fresh** SHA-256 on the final signed APK and publish it exactly as measured. Install with `adb install -r` on an existing beta.4 release (same signer) and re-run smoke and failure tests. Do not attach debug or unsigned builds to a public release.

## Real-device matrix

- Manual QUIC, HTTPS, Auto with available QUIC; Auto with UDP blocked but TCP available; both unavailable; recovery and stop during recovery.
- Screen-off idle vs forced Doze; wake, Wi-Fi→LTE→Wi-Fi, DNS/IPv6 routes, application TCP/UDP, long upload/download and high loss.
- Kill QUIC gateway, Go core and HEV separately; simulate a process that stays alive but stops serving; monitor bounded recovery without rapid reconnect loops.
- Test Always-on enabled/disabled and Android Lockdown separately; capture the **physical** interface/router for IPv4, IPv6, DNS, TCP and UDP (before, during, after VPN failure). OS Lockdown is needed to protect against full VPN-process death.
- Compare QUIC and HTTPS RTT/jitter/loss, battery drain and heartbeat frequency; compare browser vs MBox TLS/HTTP fingerprint with an instrumented test endpoint.

**Observed:** the maintainer reported passing five core connectivity scenarios. **Unobserved:** controlled process kills, independent packet capture, full signed-release acceptance and long soak. Do not mark unexecuted checks passed.
