# Testing MBox beta 4

[Русский](ru/TESTING.md) · [Android leak matrix](ANDROID_FAIL_CLOSED_TEST_PLAN.md) · [Release checklist](../RELEASE_CHECKLIST.md)

### Automated on Arch

```bash
cd ~/src/mbox-main
git diff --check
(cd client && GO111MODULE=off go test -race ./... && GO111MODULE=off go vet ./...)
(cd server && GO111MODULE=off go test -race ./... && GO111MODULE=off go vet ./...)
(cd native-quic && go test -race ./... && go vet ./...)
python3 -m pytest -q tests
./tools/secret-scan.sh
```

The maintainer reported **110 Python tests passed** and successful signed release APK build/installation. This is not independent certification and the exact released source commit must be rechecked before tagging.

### Manual on signed release APK

Fresh install/import (do not expose the `mbox://` secret), manual QUIC over Wi-Fi, manual HTTPS, Auto fallback on restricted mobile network, switch Wi-Fi↔LTE, DNS and HTTPS, Telegram, UDP/STUN if appropriate, Always-on/Lockdown. Verify transport using `adb logcat -d -s MBox:I` after sanitizing addresses/metadata. A real physical capture is needed for a positive no-leak claim.

### Field-testing status

Observed on debug: Wi-Fi QUIC, MTS/MegaFon HTTPS, Auto/reconnect, Telegram. Signed release installed with `versionName=1.0.0-beta.4`, `versionCode=7`; full signed-APK transport results were not recorded. Tele2 and long soak remain pending. Do not silently mark them passed.
