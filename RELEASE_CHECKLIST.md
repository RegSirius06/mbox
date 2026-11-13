# MBox v1.0.0-beta.4.1 — prerelease checklist

[Русский](RELEASE_CHECKLIST.ru.md) · [Detailed test plan](docs/TESTING.md) · [Known gaps](docs/BETA4_1_STATUS.md)

This is a **prerelease**, not a claim of complete P0 security validation. Keep historical beta.4 checksums unchanged in their archived files. Do not publish a debug or unsigned APK.

## Source and compatibility

- [ ] Final reviewed branch based on `v1.0.0-beta.4`; diff contains no secrets, production keys, personal endpoint assets, temporary credentials, compiled outputs or unintended rewrites.
- [ ] `VERSION`, Gradle `versionName`, `versionCode=8`, application ID, mboxctl version and release scripts agree.
- [ ] No unnecessary wire format, Noise protocol or provisioning schema changes; HTTPS-only and beta.4 QUIC profiles are tested, or migration documented.
- [ ] `git diff --check`, Go client/server race+vet, native QUIC vet/race+TLS tests, full Python tests, source layout, secret scan, Android build and unsigned APK gate all pass on the **final commit**.

## Real-device release artifact

- [ ] Build with original production keystore; verify signer pin and `io.mbox.android` identity; APK is non-debuggable and includes both ARM64 executables with no lab QUIC assets.
- [ ] In-place update of existing beta.4 signed APK with `adb install -r` works without deleting the profile.
- [ ] Exactly this signed APK passes QUIC, HTTPS, Auto (including blocked UDP but working TCP), idle/wake, network change, core/gateway/HEV failure and user stop during recovery.
- [ ] Separate Always-on and Lockdown checks completed; if independent physical IPv4/IPv6/DNS/TCP/UDP capture is missing, mark P0 *open* and disclose it prominently rather than claiming no leaks.
- [ ] Cert expiry and trusted CA migration operator plan checked before 2026-12-07; renewal timer **not deployed without operator authorization**.
- [ ] Signed APK filename and fresh SHA-256 recorded and matched with `sha256sum`; never reuse beta.4 checksum.

## Publication (owner only)

- [ ] EN and RU README, release notes, checklist and current docs reviewed separately; no mixed-language merged release document.
- [ ] Update GitHub prerelease body with exact commit/hash and explicit unfinished tests; attach **only** original-key production-signed APK and its `.sha256`.
- [ ] Create tag `v1.0.0-beta.4.1` from the final reviewed commit; push branch/tag, publish as prerelease, do **not** deploy or restart VPS.
- [ ] Verify uploaded artifact checksum against local and signer pin; record CI URL and owner field-testing evidence.
