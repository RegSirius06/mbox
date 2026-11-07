#!/usr/bin/env bash
set -euo pipefail
if [[ $# != 2 ]]; then
  echo "Usage: $0 https://IPv4:7443 /path/to/server.crt" >&2
  exit 2
fi
url="$1"; certificate="$2"
if [[ ! "$url" =~ ^https://([0-9]{1,3}\.){3}[0-9]{1,3}:[0-9]+$ ]]; then
  echo "QUIC debug endpoint must be https://numeric-ip:port" >&2
  exit 2
fi
openssl x509 -in "$certificate" -noout >/dev/null
assets="$(dirname "$0")/../app/src/debug/assets"
mkdir -p "$assets"
printf '%s\n' "$url" > "$assets/mbox-quic-remote.txt"
cp "$certificate" "$assets/mbox-quic-ca.pem"
echo "Configured debug-only QUIC endpoint. Run android/tools/build-debug.sh."
