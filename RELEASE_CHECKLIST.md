# MBox 1.0.0-beta.2.1 release checklist

[Русская версия](RELEASE_CHECKLIST.ru.md)

This checklist is for preparing `v1.0.0-beta.2.1` from the current `audit/p0-p1-fingerprint-modular` branch. Do **not** move or overwrite the released `v1.0.0-beta.2` tag; beta.2.1 is a new compatible tag.

## 1. Keep production secrets out of Git

Never stage:

- `/etc/mbox/`;
- `/var/lib/mbox/users.db*`;
- `*.psk`, private `*.key`;
- real `mbox://` links/QRs;
- generated production `server.json` / `relay.json`;
- backup/rollback archives.

Run the secret scan and manually review every provisioning-like hit.

## 2. Confirm calibrated defaults

The current real-device calibration uses:

```text
max_streams_per_session = 128
max_total_streams = 1024
```

Verify both the Go server default and `mboxctl` generated default agree:

```bash
grep -RInE 'MaxStreamsPerSession:.*128|max_streams_per_session.*128' \
  server tools/mboxctl docs README.md
```

## 3. Automated gate

```bash
cd ~/src/mbox-main
export GO111MODULE=off

for d in common/noise server client relay tools/mbox-key tools/mbox-link tools/mbox-netprobe; do
  (cd "$d" && go test ./...) || exit 1
done

(cd common/noise && go test -race ./...)
(cd server && go test -race ./...)
(cd client && go test -race -count=20 ./...)

(cd server && go vet ./...)
(cd client && go vet ./...)
(cd relay && go vet ./...)
(cd tools/mbox-netprobe && go vet ./...)

python -m pytest -q tests
python tools/check-source-layout.py
git diff --check
./tools/secret-scan.sh
```

## 4. Android build and real-device sanity check

Use Java 17:

```bash
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
hash -r
./android/tools/build-debug.sh
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

Confirm the live log contains:

```text
stack=cronet
proto=HTTP/2.0
alpn=h2
MBOX_OPEN_OK
NOISE_OK
TUNNEL_ESTABLISHED
```

Then exercise Wi-Fi/mobile handover and one airplane-mode cycle. A hard disconnect should show `WAIT_UNDERLYING`, and the server should show `SESSION SUPERSEDE` when the same device reconnects.

## 5. Documentation gate

Before commit verify:

- `CHANGELOG.md` / `CHANGELOG.ru.md`;
- `RELEASE_NOTES.md` / `.ru.md`;
- English and Russian README files;
- `docs/REG_RU_SHARED_HOSTING.md` and Russian equivalent;
- `docs/STATUS.md`, `STEALTH.md`, `SECURITY.md`, `NETWORK_COMPATIBILITY.md`, `OPERATOR_TEST.md`;
- no documentation claims that Cronet is byte-for-byte Chrome;
- no documentation claims MegaFon is fixed;
- current branch is explicitly marked **not yet field-tested under active white-list restrictions**.

## 6. Commit and push the development branch

```bash
git add -A
git diff --cached --check
git status --short
git diff --cached --stat

git commit -m 'Harden networking, add Cronet transport and robust Android handover'
git push -u origin audit/p0-p1-fingerprint-modular
```

Do not tag the next release yet. First perform the operator field matrix.

## 7. Outstanding field qualification before next tag

- active white-list tests on MTS and T2 to confirm no regression;
- MegaFon test;
- controlled `.fun` vs `.ru` on the same REG.RU shared-host backend;
- Chrome vs MBox Cronet vs MBox Go fingerprint capture;
- longer soak/load qualification and independent security review.
