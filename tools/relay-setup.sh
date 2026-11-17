#!/usr/bin/env bash
# Optional interactive shared-hosting FastCGI setup. No credentials are stored.
set -euo pipefail
umask 077

if (( EUID != 0 )); then
  echo 'Run on the origin VPS as root.' >&2
  exit 1
fi
if [[ ! -t 0 ]]; then
  echo 'Interactive terminal required; no remote changes made.' >&2
  exit 2
fi
for cmd in mboxctl sha256sum ssh; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "Missing: $cmd" >&2; exit 1; }
done

printf '\nMBox · REG.RU relay setup\n'
printf 'All values are used only for this session; no passwords are stored.\n'
read -r -p 'SSH login (user@host): ' host
if [[ ! "$host" =~ ^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+$ ]]; then
  echo 'Invalid SSH login; expected user@host.' >&2
  exit 2
fi
read -r -p 'REG.RU website root (absolute path): ' web
read -r -p 'REG.RU private directory (outside website root): ' private
if [[ "$web" != /* || "$private" != /* || "$web" == '/' || "$private" == '/' ]]; then
  echo 'Absolute, non-root paths required.' >&2
  exit 2
fi
read -r -p 'Origin server.json [/etc/mbox/server.json]: ' origin
origin=${origin:-/etc/mbox/server.json}
read -r -p 'FastCGI binary [/usr/local/lib/mbox/relay.fcgi]: ' binary
binary=${binary:-/usr/local/lib/mbox/relay.fcgi}
[[ -f "$origin" && -f "$binary" ]] || { echo 'Origin config or relay binary not found.' >&2; exit 1; }
hash=$(sha256sum -- "$binary")
hash=${hash%% *}

args=(relay deploy --ssh "$host" --remote-dir "$web" \
  --remote-private-dir "$private" --origin-config "$origin" \
  --relay-binary "$binary" --relay-sha256 "$hash")
printf '\nStep 1/2: local preflight (no remote modifications)\n'
mboxctl "${args[@]}" --dry-run
printf '\nStep 2/2: will deploy FastCGI and private HMAC key to REG.RU via SSH.\n'
printf 'Existing site content and .htaccess are preserved.\n'
read -r -p 'Proceed with deployment? Type DEPLOY: ' confirmation
if [[ "$confirmation" != DEPLOY ]]; then
  echo 'Cancelled: REG.RU unchanged.'
  exit 0
fi
mboxctl "${args[@]}"
printf '\nRelay deploy finished. The deploy command checks public HTTPS health.\n'
