#!/usr/bin/env bash
# Mandatory final gate for a publicly distributed *production-signed* APK.
# This tool never signs a build, nor does it permit a debug-key override.
set -euo pipefail

if [[ $# -ne 1 || ! -s ${1:-} ]]; then
    echo 'usage: tools/check-production-apk.sh path/to/signed-release.apk' >&2
    exit 2
fi
apk=$1
# A non-debug DN is not an identity guarantee. Refuse any release unless the
# operator pins the expected certificate SHA-256 via a trusted out-of-band path.
expected_signer=${MBOX_RELEASE_SIGNER_SHA256:-}
if [[ ! "$expected_signer" =~ ^[0-9a-fA-F]{64}$ ]]; then
    echo 'FAIL: MBOX_RELEASE_SIGNER_SHA256 must pin the expected 64-hex certificate digest' >&2
    exit 1
fi
expected_signer=${expected_signer,,}

find_tool() {
    local tool=$1 sdk dir
    if command -v "$tool" >/dev/null 2>&1; then command -v "$tool"; return 0; fi
    sdk=${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}
    if [[ -n $sdk && -d $sdk/build-tools ]]; then
        # The newest installed Android build-tools takes priority.
        while IFS= read -r dir; do
            [[ -x $dir/$tool ]] && { printf '%s\n' "$dir/$tool"; return 0; }
        done < <(find "$sdk/build-tools" -mindepth 1 -maxdepth 1 -type d | sort -Vr)
    fi
    echo "required Android SDK tool not found: $tool" >&2
    return 127
}

apksigner=$(find_tool apksigner)
aapt=$(find_tool aapt)
# Exit code on verify failure is the authoritative signature validation.
if ! signature_info=$($apksigner verify --verbose --print-certs "$apk" 2>&1); then
    echo 'FAIL: APK signature verification failed' >&2
    exit 1
fi
if ! grep -q 'certificate DN:' <<< "$signature_info"; then
    echo 'FAIL: APK signer identity was not reported' >&2
    exit 1
fi
if [[ $(grep -Ec 'Signer #[0-9]+ certificate DN:' <<< "$signature_info") -ne 1 ]]; then
    echo 'FAIL: APK must have exactly one signer' >&2
    exit 1
fi
signer_sha=$(sed -nE 's/^Signer #1 certificate SHA-256 digest: ([0-9a-fA-F]{64})$/\1/p' <<< "$signature_info")
signer_sha=${signer_sha,,}
if [[ "$signer_sha" != "$expected_signer" ]]; then
    echo 'FAIL: signer certificate SHA-256 does not match the release pin' >&2
    exit 1
fi
if grep -Eiq 'Android Debug|CN[[:space:]]*=[[:space:]]*Android Debug' <<< "$signature_info"; then
    echo 'FAIL: APK is signed with the Android debug certificate' >&2
    exit 1
fi
if ! package_info=$($aapt dump badging "$apk" 2>&1); then
    echo 'FAIL: unable to inspect Android manifest' >&2
    exit 1
fi
if ! grep -q "package: name='io.mbox.android'" <<< "$package_info"; then
    echo 'FAIL: unexpected Android application ID' >&2
    exit 1
fi
if grep -Eqi 'application-debuggable|debuggable[=: ]+true' <<< "$package_info"; then
    echo 'FAIL: APK sets android:debuggable=true' >&2
    exit 1
fi

echo 'PASS: APK signature valid; pinned signer matched; manifest non-debuggable'
sha256sum "$apk"
