# MBox `1.0.0-beta.3` prerelease checklist

[Русская версия](RELEASE_CHECKLIST.ru.md)

This checklist uses **Arch for builds/tests**, **root-managed RU VPS** for origin and a **separate REG.RU shared-hosting FastCGI** frontend. Beta.3 only updates version strings and release documentation beyond the field-tested beta.2.2 code; the released beta.2.1 Android client is incompatible with strict `cmac`.

## 1. Freeze code and run gates on Arch

```bash
cd ~/src/mbox-main
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
git status --short
cat VERSION
python3 -m pytest -q tests
python3 tools/check-source-layout.py
GO111MODULE=off go test ./common/noise ./common/controlmeta ./server ./client ./relay
GO111MODULE=off go test -race ./common/controlmeta ./server ./client ./relay
GO111MODULE=off go vet ./common/noise ./common/controlmeta ./server ./client ./relay
./tools/secret-scan.sh
git diff --check
```

For a release, review actual CI, Android build, field leak/Lockdown risks and the open items in `docs/BETA_3_RELEASE_STATUS.md`. Unit tests alone do not establish fail-closed behavior.

## 2. Update Android, check origin and REG.RU

```bash
./android/tools/build-debug.sh
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
adb shell dumpsys package io.mbox.android | grep -E 'versionCode|versionName'
```

Expected: `versionCode=6`, `versionName=1.0.0-beta.3`. Verify Internet/TG, reconnect and device reboot. **Exception for this experimental beta.3 prerelease:** we explicitly ship the debug-signed artifact with a `-debug.apk` filename and prominent warnings. It is not a production release build.

For the root-managed RU VPS, rebuild from the beta.3 tree and deploy only if validation succeeds:

```bash
mkdir -p dist/vps
(cd server && GO111MODULE=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o ../dist/vps/mbox-server .)
tools/deploy-origin-root.sh --host root@YOUR_RU_VPS
# Only once dry-run and build succeed:
tools/deploy-origin-root.sh --host root@YOUR_RU_VPS --apply
```

This changes only origin server executable and `mboxctl`; **do not redeploy REG.RU FastCGI** for this version-only delta. Record `mboxctl doctor`, running binary SHA and whether `auth.require_authenticated_control=true` remains effective.

## 3. Prepare explicitly labelled experimental debug APK

For beta.3 only, the operator has chosen to publish the debug-signed APK. Do not confuse it with a production-signed release. The debug APK can be debuggable and is signed with a development key; changing to a release certificate later usually prevents in-place upgrades. Preserve credentials/profiles securely before reinstalling.

```bash
mkdir -p dist/release
cp android/app/build/outputs/apk/debug/app-debug.apk dist/release/MBox-1.0.0-beta.3-android-arm64-debug.apk
(cd dist/release && sha256sum MBox-1.0.0-beta.3-android-arm64-debug.apk > MBox-1.0.0-beta.3-android-arm64-debug.apk.sha256)
```

The regular `tools/build-release-apk.sh` and `tools/check-production-apk.sh` remain intact for a future signed production APK; do not bypass their security checks by renaming a debug APK.

## 4. Publish via Pull Request, then tag merged main

```bash
gh auth status
git status --short
git diff --check
git fetch origin main
# Branch from the local hardening history; do not push it directly onto main.
git push -u origin HEAD:audit/hardening-2026-11-04
gh pr create --repo RegSirius06/mbox --base main --head audit/hardening-2026-11-04 --title 'MBox 1.0.0-beta.3 experimental prerelease' --body-file RELEASE_NOTES.ru.md
gh pr checks --watch
# Review the PR before merging. Keep feature branch for easy comparison and rollback.
gh pr merge --merge
git fetch origin main
# Verify the merge commit contains beta.3:
git show origin/main:VERSION
git tag -a v1.0.0-beta.3 origin/main -m 'MBox 1.0.0-beta.3 experimental prerelease'
git push origin v1.0.0-beta.3
gh release create v1.0.0-beta.3 \
  dist/release/MBox-1.0.0-beta.3-android-arm64-debug.apk \
  dist/release/MBox-1.0.0-beta.3-android-arm64-debug.apk.sha256 \
  --repo RegSirius06/mbox \
  --title 'MBox 1.0.0-beta.3 (experimental debug APK)' \
  --notes-file RELEASE_NOTES.ru.md --prerelease --verify-tag
```

Stop on any error; do not force-push the main branch, and never put the private keystore, app profile, PSKs, runtime secrets, or server database in Git or GitHub Releases. After merging, verify tag and release asset names with `gh release view v1.0.0-beta.3 --json tagName,isPrerelease,assets,url`.

## 5. Limitations to declare to testers

TCP-only; UDP-dependent apps may fail. Do not claim leak-free VPN: Android Lockdown + physical packet capture still pending. Distinguish REG.RU metadata visibility from ISP DPI. Do not distribute reusable PSK profiles in public issue threads. Keep the deployment credentials and databases out of Git.
