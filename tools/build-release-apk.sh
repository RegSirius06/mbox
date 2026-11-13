#!/usr/bin/env bash
# Release build/signing on Arch. No keys are stored in the repository.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${MBOX_RELEASE_KEYSTORE:?Set MBOX_RELEASE_KEYSTORE to a keystore outside the repository}"
: "${MBOX_RELEASE_KEY_ALIAS:?Set MBOX_RELEASE_KEY_ALIAS}"
: "${MBOX_RELEASE_SIGNER_SHA256:?Set trusted MBOX_RELEASE_SIGNER_SHA256 (cert digest)}"
[[ -f "$MBOX_RELEASE_KEYSTORE" ]] || { echo 'Keystore missing' >&2; exit 1; }
case "$(realpath "$MBOX_RELEASE_KEYSTORE")" in "$ROOT"/*) echo 'Keystore must be outside the checkout' >&2; exit 1;; esac
find_android_tool() {
  local tool=$1 sdk dir
  if command -v "$tool" >/dev/null 2>&1; then command -v "$tool"; return 0; fi
  sdk=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
  if [[ -n $sdk && -d $sdk/build-tools ]]; then
    while IFS= read -r dir; do
      [[ -x $dir/$tool ]] && { printf '%s\n' "$dir/$tool"; return 0; }
    done < <(find "$sdk/build-tools" -mindepth 1 -maxdepth 1 -type d | sort -Vr)
  fi
  echo "Android SDK tool not found: $tool; set ANDROID_HOME or add build-tools to PATH" >&2
  return 127
}
APKSIGNER=$(find_android_tool apksigner)
ZIPALIGN=$(find_android_tool zipalign)
[[ "$MBOX_RELEASE_SIGNER_SHA256" =~ ^[0-9a-fA-F]{64}$ ]] || { echo 'Expected 64-character certificate SHA-256 pin' >&2; exit 1; }
VERSION="$(cat "$ROOT/VERSION")"
[[ "$VERSION" == 1.0.0-beta.4.1 ]] || { echo "Unexpected version: $VERSION" >&2; exit 1; }
export JAVA_HOME="${JAVA_HOME:-/usr/lib/jvm/java-17-openjdk}"
export PATH="$JAVA_HOME/bin:$PATH"
umask 077
"$ROOT/android/tools/build-core.sh"
(cd "$ROOT/android" && ./gradlew assembleRelease)
UNSIGNED="$ROOT/android/app/build/outputs/apk/release/app-release-unsigned.apk"
[[ -s "$UNSIGNED" ]] || { echo "Missing unsigned release APK: $UNSIGNED" >&2; exit 1; }
# Fail closed before signing: native QUIC must exist and personal debug
# endpoints/certificates must NOT be shipped in the public APK.
python3 - "$UNSIGNED" <<'PYAPK'
import sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as apk:
    names=set(apk.namelist())
    for n in ('lib/arm64-v8a/libmbox.so', 'lib/arm64-v8a/libmboxquic.so'):
        if n not in names: raise SystemExit('FAIL: required native binary missing: '+n)
    for n in ('assets/mbox-quic-remote.txt', 'assets/mbox-quic-ca.pem'):
        if n in names: raise SystemExit('FAIL: debug-only QUIC asset in release: '+n)
print('PASS: release contains core and QUIC; no debug endpoint assets')
PYAPK
OUT="$ROOT/dist/release"
mkdir -p "$OUT"
TMP="$(mktemp -d "$OUT/.mbox-sign.XXXXXXXX")"
trap 'rm -rf -- "$TMP"' EXIT
"$ZIPALIGN" -P 16 -f 4 "$UNSIGNED" "$TMP/aligned.apk"
ARTIFACT="$OUT/MBox-$VERSION-android-arm64.apk"
[[ ! -e "$ARTIFACT" ]] || { echo "Release artifact already exists: $ARTIFACT (remove deliberately if rebuilding)" >&2; exit 1; }
# apksigner prompts securely for the keystore password if needed. Never pass
# keystore passwords on the CLI or log them.
"$APKSIGNER" sign --ks "$MBOX_RELEASE_KEYSTORE" --ks-key-alias "$MBOX_RELEASE_KEY_ALIAS" --out "$TMP/signed.apk" "$TMP/aligned.apk"
"$ROOT/tools/check-production-apk.sh" "$TMP/signed.apk"
install -m 0644 "$TMP/signed.apk" "$ARTIFACT"
(cd "$OUT" && sha256sum "$(basename "$ARTIFACT")" > "$(basename "$ARTIFACT").sha256")
echo "Release APK ready: $ARTIFACT"
echo 'Verify certificate pin against a trusted separately recorded digest before publishing.'
