#!/usr/bin/env bash
# Mandatory final gate for a publicly distributed *production-signed* APK.
# This tool never signs a build, nor does it permit a debug-key override.
set -euo pipefail

if [[ $# -ne 1 || ! -s ${1:-} ]]; then
    echo 'usage: tools/check-production-apk.sh path/to/signed-release.apk' >&2
    exit 2
fi
apk=$1

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

echo 'PASS: signature valid; release identity present; not a debug certificate; manifest non-debuggable'
sha256sum "$apk"
