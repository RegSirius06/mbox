# MBox 1.0.0-beta.2 release checklist

## 1. Keep production secrets out of the source tree

Do **not** copy these from the live server into Git:

- `/etc/mbox/`
- `/var/lib/mbox/users.db*`
- `*.psk`, `*.key`, provisioning links/QRs
- generated `server.json`, `relay.json`
- backup/rollback archives

The public source tree should contain code, examples and documentation only.

## 2. Optional: compare the live source tree with Arch

The Arch tree is already beta.2, so normally no production state needs to be pulled. If you want to compare exact source files from the server, copy them into a separate directory first:

```bash
mkdir -p ~/src/mbox-server-copy
rsync -a --delete \
  --exclude '.git/' \
  --exclude 'mbox-1.0.0-beta.2.patch' \
  root@YOUR_SERVER:/root/mbox-new/mbox-main/ \
  ~/src/mbox-server-copy/

diff -qr ~/src/mbox-main ~/src/mbox-server-copy \
  --exclude=.git --exclude=build --exclude=.gradle --exclude='*.apk' --exclude='*.so'
```

Never rsync `/etc/mbox` or `/var/lib/mbox` into the repository.

## 3. Apply the final documentation/release patch

From the Arch beta.2 source tree:

```bash
cd ~/src/mbox-main
git apply --check /path/to/mbox-beta2-release-docs.patch
git apply /path/to/mbox-beta2-release-docs.patch
```

## 4. Build/test on Arch

```bash
cd ~/src/mbox-main
export GO111MODULE=off

# Core Go checks
for d in common/noise server client relay; do
  (cd "$d" && go test ./...)
done
(cd server && go vet ./...)
(cd client && go vet ./...)
(cd relay && go vet ./...)

# Python CLI tests
python -m pytest -q tests/test_mboxctl_helpers.py

# Secret scan
./tools/secret-scan.sh
# The scanner intentionally reports literal `mbox://` references for manual review;
# that warning is expected. It must not report real PSKs/keys/databases/backups.

# Android: force Java 17 for this project
sudo pacman -S --needed jdk17-openjdk
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
hash -r
cd android
./gradlew --stop
./gradlew --version
./tools/build-debug.sh
sha256sum app/build/outputs/apk/debug/app-debug.apk
cd ..
```

Verify `./gradlew --version` reports Java 17 before the APK build.

## 5. Sanity-check the live server

```bash
ssh root@YOUR_SERVER
cd ~/mbox-new/mbox-main
cat VERSION
sudo mboxctl doctor
sudo mboxctl resources
sudo mboxctl user list
journalctl -u mbox-server --since '30 minutes ago' --no-pager \
  | grep 'handshake=Noise_NKpsk0_25519_AESGCM_SHA256 record=v2'
```

Expected source version: `1.0.0-beta.2`.

## 6. Prepare Git history

```bash
cd ~/src/mbox-main
rm -f mbox-1.0.0-beta.2.patch

git init                    # only if this tree is not already a repository
git branch -M main
git add .
git status --short
git diff --cached --check
git diff --cached --stat
```

Before commit, explicitly confirm that the index contains no live database, key, PSK, runtime config or backup archive.

```bash
git commit -m 'MBox 1.0.0-beta.2: Noise-only transport and reliability hardening'
git tag -a v1.0.0-beta.2 -m 'MBox 1.0.0-beta.2'
```

## 7. Push to GitHub

With GitHub CLI:

```bash
gh auth status
gh repo create mbox --private --source=. --remote=origin --push
git push origin v1.0.0-beta.2
```

If the repository already exists:

```bash
git remote add origin git@github.com:YOUR_USER/mbox.git   # only once
git push -u origin main
git push origin v1.0.0-beta.2
```

Keep it private for the first upload. Make it public only after reviewing staged/history secrets and third-party licensing.

## 8. Suggested GitHub release text

Use `RELEASE_NOTES.md` as the release body. The known issue must retain the precise wording: unrestricted Wi-Fi was only the baseline; MTS worked under white-list restrictions in this field test; MegaFon did not.
