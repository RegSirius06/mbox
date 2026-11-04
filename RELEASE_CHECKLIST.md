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

Expected: `versionCode=6`, `versionName=1.0.0-beta.3`. A debug APK is for local testing only. Verify Internet/TG, reconnect and device reboot. The developer-provided release build must be signed separately.

For the root-managed RU VPS, rebuild from the beta.3 tree and deploy only if validation succeeds:

```bash
mkdir -p dist/vps
(cd server && GO111MODULE=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o ../dist/vps/mbox-server .)
tools/deploy-origin-root.sh --host root@YOUR_RU_VPS
# Only once dry-run and build succeed:
tools/deploy-origin-root.sh --host root@YOUR_RU_VPS --apply
```

This changes only origin server executable and `mboxctl`; **do not redeploy REG.RU FastCGI** for this version-only delta. Record `mboxctl doctor`, running binary SHA and whether `auth.require_authenticated_control=true` remains effective.

## 3. Sign a public APK, never use the debug artifact

Use a protected Android **release keystore** stored outside the repo and a separately verified expected signer cert SHA-256. Do **not** paste keystore passwords or private keys into chat or commit them.

```bash
export MBOX_RELEASE_KEYSTORE=/secure/path/mbox-release.jks
export MBOX_RELEASE_KEY_ALIAS=mbox-release
export MBOX_RELEASE_SIGNER_SHA256=YOUR_EXPECTED_64_HEX_CERT_SHA256
./tools/build-release-apk.sh
```

This invokes release assembly, aligns and signs the APK, checks the expected signer digest, non-debuggable manifest, and writes an artifact checksum. For updates in-place, signer must match the APK already installed; a debug-signed test APK cannot be upgraded in-place to a release-signed APK with another certificate. Back up/export the profile before uninstalling, and avoid sharing PSKs.

## 4. Publish Git commit, tag and prerelease via GitHub CLI (`gh`)

After all checks, on Arch:

```bash
gh auth status
git status --short
# If beta.3 was applied with git am, the release changes are already committed.
# Do not create an empty duplicate commit.
git status --short
git log -3 --oneline
# Only commit additional local edits, if any, after inspecting them.
git diff --check

git fetch origin main
# STOP if this fails: do not force-push or overwrite remote history.
git merge-base --is-ancestor origin/main HEAD
# Publish all release source/docs on the default branch:
git push origin HEAD:main

git tag -a v1.0.0-beta.3 -m 'MBox 1.0.0-beta.3 experimental prerelease'
git push origin v1.0.0-beta.3

gh release create v1.0.0-beta.3 \
  dist/release/MBox-1.0.0-beta.3-android-arm64.apk \
  dist/release/MBox-1.0.0-beta.3-android-arm64.apk.sha256 \
  --title 'MBox 1.0.0-beta.3 (experimental prerelease)' \
  --notes-file RELEASE_NOTES.ru.md --prerelease --verify-tag
```

If branch protection rejects a direct push or main has diverged, create a PR instead and tag the merge commit **after** merging. Never tag a debug APK or force-push an existing release. Verify remote tag and release assets with `gh release view v1.0.0-beta.3 --json tagName,isPrerelease,assets,url`.

## 5. Limitations to declare to testers

TCP-only; UDP-dependent apps may fail. Do not claim leak-free VPN: Android Lockdown + physical packet capture still pending. Distinguish REG.RU metadata visibility from ISP DPI. Do not distribute reusable PSK profiles in public issue threads. Keep the deployment credentials and databases out of Git.
