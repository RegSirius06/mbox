#!/usr/bin/env bash
set -euo pipefail
ANDROID_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO_ROOT="$(cd "$ANDROID_ROOT/.." && pwd)"
OUT="$ANDROID_ROOT/app/src/main/jniLibs/arm64-v8a/libmbox.so"
mkdir -p "$(dirname "$OUT")"
cd "$REPO_ROOT/client"
GO111MODULE=off GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags='-s -w' -o "$OUT" *.go
file "$OUT"
sha256sum "$OUT"
# One native QUIC/HTTP3 executable, shared by debug and release variants.
# Old local debug outputs must be removed: duplicate JNI paths break Gradle packaging.
QUIC_DEBUG_OLD="$ANDROID_ROOT/app/src/debug/jniLibs/arm64-v8a/libmboxquic.so"
rm -f -- "$QUIC_DEBUG_OLD"
QUIC_OUT="$ANDROID_ROOT/app/src/main/jniLibs/arm64-v8a/libmboxquic.so"
mkdir -p "$(dirname "$QUIC_OUT")"
(cd "$REPO_ROOT/native-quic" && GO111MODULE=on GOOS=android GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags='-s -w' -o "$QUIC_OUT" .)
file "$QUIC_OUT"
sha256sum "$QUIC_OUT"
