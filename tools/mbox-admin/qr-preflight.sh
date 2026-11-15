#!/usr/bin/env bash
# Run under root on VPS. Safe diagnostics: no subscription URLs or PSKs printed.
set -euo pipefail
if ! command -v qrencode >/dev/null 2>&1; then
  echo 'MISSING: qrencode (install: apt-get update && apt-get install -y qrencode)'
  exit 2
fi
TEST=$(mktemp)
trap 'rm -f "$TEST"' EXIT
printf 'https://example.invalid/test' | qrencode -8 -t PNG -o "$TEST" -s 5
python3 - "$TEST" <<'PY'
from pathlib import Path
import sys
raw=Path(sys.argv[1]).read_bytes()
assert raw.startswith(b'\x89PNG\r\n\x1a\n'), 'qrencode returned non-PNG'
print(f'QR PNG generator OK ({len(raw)} bytes)')
PY
