# MBox 1.0.0-beta.4.3 release engineering

[Русский](ru/RELEASE_ENGINEERING_BETA43.md) · [Checklist](../RELEASE_CHECKLIST.md)

**Build and sign on the maintainer's trusted Arch machine.** The production keystore must remain outside Git. These instructions do not update the origin VPS, push to GitHub, or publish a release.

## Version alignment

The final commit must contain `VERSION=1.0.0-beta.4.3`, Android `applicationId='io.mbox.android'`, `versionCode=10`, `versionName='1.0.0-beta.4.3'`; `mboxctl version` in source and both APK gate scripts must agree. Current EN/RU entry-point documentation must refer to beta.4.3. Older beta.4.1/4.2 historical docs should not be rewritten. The **deployed VPS** remains at its previous version until explicitly upgraded; a new web UI alone is not an origin upgrade.

## Source and toolchain checks

```bash
cd ~/src/mbox-main
set +e    # Avoid leaving interactive Bash with errexit enabled

git status --short
git diff --check
python3 -m pytest -q tests
GO111MODULE=off go test -race ./client ./server ./relay
(
  cd native-quic
  go vet ./...
  go test -race -count=1 ./...
)
bash -n install.sh tools/relay-setup.sh tools/build-release-apk.sh
python3 tools/check-release-docs.py
```

Inspect for leaked secrets and temporary files before staging. `tools/secret-scan.sh` can print expected warnings for example `mbox://` documentation links and is not a substitute for human review. Avoid blind `git add -A`.

## Build with the existing production signing identity

Pinned certificate SHA-256 verified previously:
`17FD9F930B8356BECA8251DD215220771A3D020E56FB6A703B526C35CCCD7586`

This is the **signer certificate fingerprint**, not the APK file hash. Verify it against your separately trusted record. `keytool` requests the keystore password locally:

```bash
cd ~/src/mbox-main
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
export MBOX_RELEASE_KEYSTORE="$HOME/.local/share/mbox-signing/mbox-release.p12"
export MBOX_RELEASE_KEY_ALIAS='mbox-release'
export MBOX_RELEASE_SIGNER_SHA256='17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586'

test -f "$MBOX_RELEASE_KEYSTORE" && echo 'KEY FOUND' || echo 'KEY MISSING: stop before signing'
keytool -list -v -storetype PKCS12 -keystore "$MBOX_RELEASE_KEYSTORE" \
  -alias "$MBOX_RELEASE_KEY_ALIAS" | grep -E 'Alias name:|SHA256:'
```

Never put the keystore password in a command line, Git or a chat. `tools/build-release-apk.sh` uses `apksigner` to request it securely.

The release script intentionally refuses to overwrite an existing APK. Back up an older beta.4.3 artifact and its checksum first:

```bash
cd ~/src/mbox-main
APK='dist/release/MBox-1.0.0-beta.4.3-android-arm64.apk'
BACKUP="$HOME/mbox-apk-backups/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p -- "$BACKUP"
for f in "$APK" "$APK.sha256"; do
  if [[ -f "$f" ]]; then mv -- "$f" "$BACKUP/"; fi
done
./tools/build-release-apk.sh
```

This builds the native core/QUIC components, the release APK, aligns, signs, and validates the package. **Gradle `BUILD SUCCESSFUL` does not mean the new APK was signed.** Require the final `Release APK ready` message.

## Verify and test on a real phone

```bash
cd ~/src/mbox-main
APK='dist/release/MBox-1.0.0-beta.4.3-android-arm64.apk'
test -s "$APK" && echo "SIGNED APK FOUND" || echo "SIGNED APK MISSING: stop here"
./tools/check-production-apk.sh "$APK"
( cd dist/release && sha256sum -c MBox-1.0.0-beta.4.3-android-arm64.apk.sha256 )
sha256sum "$APK"
adb install -r "$APK"
```

Confirm in-place installation preserves app data, profiles and subscriptions; verify system light/dark switching while connected, QUIC, HTTPS, AUTO fallback, network loss and handover, idle resume, and the notification Disconnect action. Local source tests do **not** replace testing this exact signed APK. Physical IPv4/IPv6/DNS/UDP leak capture and full Lockdown checks remain separate acceptance gates unless independently recorded.

## Deployed origin and shared hosting

The updated admin UI is installed on the maintainer's VPS, but `mboxctl version` previously reported beta.4.2. This is a real **deployment-state mismatch**, not merely an old HTML label. After a verified backup, use `./install.sh --check` and plan a separately approved `--upgrade-origin` on the VPS. That command restarts the origin and can disconnect users. Do not bundle it into APK publication or silently push changes to REG.RU shared hosting.

## Publishing

After final review: commit/push, green CI, tag `v1.0.0-beta.4.3`, and a GitHub Release marked **Pre-release**. Attach only the production-signed ARM64 APK and `.sha256`. Replace `PENDING` in `docs/GITHUB_RELEASE_BODY.md` and `docs/ru/GITHUB_RELEASE_BODY.md` with the **actual** APK checksum and commit SHA; never publish secrets, private profiles or signing material.
