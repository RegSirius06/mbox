#!/usr/bin/env bash
set -euo pipefail
ANDROID_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$ANDROID_ROOT/.." && pwd)"
OUT="$ANDROID_ROOT/app/src/main/jniLibs/arm64-v8a/libmbox.so"
mkdir -p "$(dirname "$OUT")"
cd "$REPO_ROOT/client"
GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags='-s -w' -o "$OUT" *.go
file "$OUT"
sha256sum "$OUT"
