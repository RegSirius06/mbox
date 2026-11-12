#!/usr/bin/env bash
# MBox APK build entrypoint. The debug flag changes the build type and UI.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
case "${1:-}" in
    --debug)
        [[ $# == 1 ]] || { echo 'usage: tools/build-apk.sh [--debug|--release]' >&2; exit 2; }
        exec "$ROOT/android/tools/build-debug.sh"
        ;;
    ''|--release)
        [[ $# -le 1 ]] || { echo 'usage: tools/build-apk.sh [--debug|--release]' >&2; exit 2; }
        # Do not quietly publish a release without the promised native QUIC.
        if [[ ! -s "$ROOT/android/app/src/main/jniLibs/arm64-v8a/libmboxquic.so" ]]; then
            echo 'RELEASE BLOCKED: libmboxquic.so is still debug-only.' >&2
            echo 'Migrate native QUIC into the release build with per-profile settings first.' >&2
            echo 'For the current laboratory APK use: tools/build-apk.sh --debug' >&2
            exit 1
        fi
        exec "$ROOT/tools/build-release-apk.sh"
        ;;
    -h|--help)
        echo 'usage: tools/build-apk.sh [--debug|--release]'
        echo '  (default) release: compact UI, production signature, only after QUIC release integration'
        echo '  --debug: full laboratory UI, Android debug signature'
        ;;
    *)
        echo 'usage: tools/build-apk.sh [--debug|--release]' >&2
        exit 2
        ;;
esac
