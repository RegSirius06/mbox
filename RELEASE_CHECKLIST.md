# MBox v1.0.0-beta.4.4 — final release gate

[Русский](RELEASE_CHECKLIST.ru.md) · [Release notes](RELEASE_NOTES.md) · [Release body](docs/GITHUB_RELEASE_BODY.md)

**Pre-release, not RC1.** Perform on the **final source commit** and production-signed APK. A box may be checked only with actual evidence.

## Source, security and docs

- [ ] `VERSION` = `1.0.0-beta.4.4`; Android `versionName=1.0.0-beta.4.4`, `versionCode=11`, package `io.mbox.android`; CLI and release verification gate agree.
- [ ] No private credentials, `mbox://`, subscription tokens, keystore, server.json, local egress.json or database files in git; `git diff --check` and `tools/secret-scan.sh` pass.
- [ ] `python3 tools/check-release-docs.py` and `python3 tools/check-source-layout.py` pass; RU/EN current docs updated. Dated beta.4.1–4.3 pages are historical, not rewritten as present-day results.
- [ ] `GO111MODULE=off go test -race ./common/dgram ./common/noise ./client ./server ./relay`; `go vet` pass; `(cd native-quic && go vet ./... && go test -race -count=1 ./...)` pass; `python3 -m pytest -q tests` evaluated and all failures resolved or documented.
- [ ] `bash -n install.sh` and `bash install.sh --check` pass on developer machine. **Fresh install and upgrade/rollback require separate clean VPS/staging acceptance**; read-only check alone is not proof that install.sh works end to end.

## Android, transports and compatibility

- [ ] Build release using the **original** keystore/pinned signing cert (not debug); `tools/check-production-apk.sh` passes; APK and `.sha256` match.
- [ ] `adb install -r` from the production-signed beta.4.3 retains user profiles/subscriptions; no uninstall; UI (EN/RU/light/dark), hidden diagnostics and Choose profile spacing verified.
- [ ] Ingress QUIC and HTTPS, AUTO fallback, physical network loss/handover, screen-off idle, recovery, QUIC idle/lane reattach tested with same Noise session.
- [ ] UDP AUTO, STREAMS only and DATAGRAM only (including **no UDP streams fallback in either direction**) tested on new origin; TCP and authenticated control still work.
- [ ] Subscription v1 to beta.4.3, v2 to beta.4.4, `MBD1` and `MBD2` accepted as intended. A single URL yields DK and KZ profiles; real exit IP/country verified; default route still works for legacy.
- [ ] GitHub update notification tested on **previous beta.4.3 APK after publication** with VPN active, including no repeat prompts, dismiss action and correct release URL. If notifications are disabled, document fallback link.

## Operations and known risks

- [ ] Origin updated separately with backup/rollback, data/key preservation, DATAGRAM socket/ACL; admin/subscriptions and REG.RU remain independent; multiple clients work after deployment.
- [ ] Shared-host relay `tools/build-relay.sh` self-check passes; a live REG.RU deployment is **not** implied. No unapproved frontend changes or optimistic CPU claims.
- [ ] `egress.json` remains readable by origin user (`root:mbox 0640`) after editing in panel; default exit and `egress_id` allowlist checked.
- [ ] Open public issues for REG.RU FastCGI CPU/worker resource failure risk, Android battery measurement, physical IPv4/IPv6/DNS/UDP leak/Lockdown verification, release update test and exit UI cleanup.

## Publish

- [ ] GitHub `main` contains the exact tested source and is green in CI; tag `v1.0.0-beta.4.4` points at it (not stale beta.4.3).
- [ ] Publish GitHub Release with **Pre-release** checked; attach only correctly signed APK and SHA256 file. Fill `PENDING_*` in English/Russian release body from actual assets. No debug binaries or tokens.
- [ ] Record APK SHA, tag SHA, signed APK install evidence and issue links. Recheck download + update alert on an old device.
