#!/usr/bin/env bash
# Read-only final release preflight. Never pushes/tags/creates a release.
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
[[ $(cat VERSION) == '1.0.0-beta.4.4' ]] || { echo 'FAIL: wrong version' >&2; exit 1; }
[[ -z $(git status --porcelain) ]] || { echo 'FAIL: uncommitted changes/untracked files. Review and commit the final source first.' >&2; git status --short; exit 1; }
python3 tools/check-release-docs.py
python3 tools/check-source-layout.py
git diff --check
APK="dist/release/MBox-1.0.0-beta.4.4-android-arm64.apk"
[[ -s "$APK" ]] || { echo "FAIL: missing signed $APK" >&2; exit 1; }
[[ -s "$APK.sha256" ]] || { echo "FAIL: missing $APK.sha256" >&2; exit 1; }
(cd "$(dirname "$APK")" && sha256sum --check "$(basename "$APK").sha256")
"$ROOT/tools/check-production-apk.sh" "$APK"
SHA=$(git rev-parse HEAD)
TAG=v1.0.0-beta.4.4
if git rev-parse --verify --quiet "refs/tags/$TAG" >/dev/null; then
  [[ $(git rev-list -n1 "$TAG") == "$SHA" ]] || { echo "FAIL: $TAG points to different SHA" >&2; exit 1; }
fi
printf 'READY_FOR_MANUAL_REVIEW=1\nCOMMIT=%s\nAPK=%s\nAPK_SHA256=%s\n' "$SHA" "$APK" "$(sha256sum "$APK" | cut -d' ' -f1)"
echo 'Verify CI, actual signed-APK beta.4.3 upgrade, original signer, issues, docs and post-release notification before publishing.'
