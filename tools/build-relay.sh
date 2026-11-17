#!/usr/bin/env bash
# Build the REG.RU FastCGI relay from the complete MBox source tree.
set -euo pipefail
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${1:-$ROOT/build/relay.fcgi}"
if [[ "$OUT" != /* ]]; then OUT="$PWD/$OUT"; fi
if [[ ! -f "$ROOT/common/originauth/auth.go" || ! -f "$ROOT/relay/origin_auth.go" ]]; then
  echo 'ERROR: run this script inside the complete MBox repository (relay/ and common/ required)' >&2
  exit 1
fi
TMP="$(mktemp -d)"
trap 'rm -rf -- "$TMP"' EXIT
(
  cd "$ROOT/relay"
  GO111MODULE=off go test ./...
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=off \
    go build -trimpath -ldflags='-s -w' -o "$TMP/relay.fcgi" .
)
# Verify that the compiled binary understands the current runtime_dir schema.
python3 - "$TMP/relay.fcgi" <<'PY'
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

binary = sys.argv[1]
with tempfile.TemporaryDirectory(prefix='mbox-relay-build-test-') as tmp:
    root = Path(tmp)
    secret = root / 'origin-hmac.key'
    secret.write_text('00' * 32 + '\n')
    secret.chmod(0o600)
    locks = root / 'locks'
    locks.mkdir(mode=0o700)
    config = root / 'relay.json'
    config.write_text(json.dumps({
        'target': 'https://example.invalid',
        'allowed_exact': ['/edge/test/health'],
        'allowed_prefixes': ['/edge/test/api/v1/'],
        'max_slots': 1,
        'max_body_bytes': 2048,
        'runtime_dir': str(locks),
        'origin_hmac_secret_file': str(secret),
    }))
    config.chmod(0o600)
    env = os.environ.copy()
    env['MBOX_RELAY_CONFIG'] = str(config)
    try:
        result = subprocess.run([binary, '--check-config'], env=env,
                                stdin=subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                timeout=5, check=False)
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise SystemExit(f'ERROR: relay config self-check could not run: {type(exc).__name__}')
    if result.returncode or result.stdout.strip() != b'MBOX_RELAY_CONFIG_OK':
        raise SystemExit('ERROR: relay config self-check failed; binary/config ABI mismatch')
print('RELAY_CONFIG_SELF_CHECK_OK')
PY
mkdir -p -- "$(dirname -- "$OUT")"
install -m 0755 "$TMP/relay.fcgi" "$OUT"
sha256sum -- "$OUT"
echo "RELAY_BUILD_OK: $OUT"
