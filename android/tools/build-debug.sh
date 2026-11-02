#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
(cd "$ROOT/app/libs" && sha256sum -c hev-socks5-tunnel.aar.sha256)
"$ROOT/tools/build-core.sh"
cd "$ROOT"
./gradlew assembleDebug
ls -lh app/build/outputs/apk/debug/app-debug.apk
