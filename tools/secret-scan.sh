#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT"
rc=0
bad_files=$(find . -type f \( -name '*.psk' -o -name '*.key' -o -name '*.mbox' -o -name '*.db' -o -name 'server.json' -o -name 'mbox.conf' -o -name 'relay.json' \) -not -path './.git/*' -print || true)
if [[ -n "$bad_files" ]]; then
  echo "Potential secret/runtime files present in repository tree:" >&2
  echo "$bad_files" >&2
  rc=1
fi
if grep -RIn --exclude-dir=.git --exclude='secret-scan.sh' 'mbox://' . >/tmp/mbox-secret-scan-links.$$ 2>/dev/null; then
  if [[ -s /tmp/mbox-secret-scan-links.$$ ]]; then
    echo "Literal mbox:// content found; review it:" >&2
    cat /tmp/mbox-secret-scan-links.$$ >&2
  fi
fi
rm -f /tmp/mbox-secret-scan-links.$$
exit "$rc"
