#!/usr/bin/env bash
# Read-only drift audit using ONE SSH connection. NEVER puts private state in Git.
set -euo pipefail
ROOT=${1:-$HOME/src/mbox-main}
HOST=${2:-root@84.54.44.148}
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
OUT=${3:-$HOME/mbox-vps-source-audit-$STAMP}
[[ -f "$ROOT/VERSION" && -d "$ROOT/tools/mbox-admin" ]] || { echo 'Not an MBox checkout' >&2; exit 2; }
umask 077
mkdir -p "$OUT/raw" "$OUT/from-vps/tools/mbox-admin" "$OUT/from-vps/tools/mbox-subscription" "$OUT/from-vps/ops"
# Use one SSH authentication. Only explicit source and system files, never
# /etc/mbox, databases, certificate private keys or provisioning secrets.
ssh "$HOST" 'tar -C / --ignore-failed-read -cf - \
 usr/local/lib/mbox-admin/mbox_admin.py \
 usr/local/lib/mbox-admin/index.html \
 usr/local/lib/mbox-admin/config_ui.py \
 usr/local/bin/mbox-subscription \
 root/mbox-beta42-src/tools/mbox-subscription/caddy_route.py \
 etc/caddy/Caddyfile \
 etc/systemd/system/mbox-admin.service \
 etc/systemd/system/mbox-admin.service.d \
 etc/systemd/system/mbox-subscription.service' > "$OUT/source-and-ops.tar"
tar -xf "$OUT/source-and-ops.tar" -C "$OUT/raw"
copy_if_present() {
  local source=$1 target=$2
  [[ ! -e "$OUT/raw/$source" || -L "$OUT/raw/$source" ]] && return 0
  cp -p "$OUT/raw/$source" "$OUT/from-vps/$target"
}
copy_if_present usr/local/lib/mbox-admin/mbox_admin.py tools/mbox-admin/mbox_admin.py
copy_if_present usr/local/lib/mbox-admin/index.html tools/mbox-admin/index.html
copy_if_present usr/local/lib/mbox-admin/config_ui.py tools/mbox-admin/config_ui.py
copy_if_present usr/local/bin/mbox-subscription tools/mbox-subscription/mbox_subscription.py
copy_if_present root/mbox-beta42-src/tools/mbox-subscription/caddy_route.py tools/mbox-subscription/caddy_route.py
copy_if_present etc/caddy/Caddyfile ops/Caddyfile.live
{
 echo "Source drift audit $(date -u +%FT%TZ)"
 echo "Arch checkout: $ROOT"
 echo "VPS snapshot: $OUT/from-vps"
 for rel in tools/mbox-admin/mbox_admin.py tools/mbox-admin/index.html \
            tools/mbox-admin/config_ui.py tools/mbox-subscription/mbox_subscription.py \
            tools/mbox-subscription/caddy_route.py; do
    if [[ ! -e "$OUT/from-vps/$rel" ]]; then echo "MISSING_VPS $rel"
    elif [[ ! -e "$ROOT/$rel" ]]; then echo "ONLY_VPS $rel"
    elif cmp -s "$ROOT/$rel" "$OUT/from-vps/$rel"; then echo "MATCH $rel"
    else echo "DIFF $rel";fi
 done
} | tee "$OUT/summary.txt"
echo 'Nothing was changed in the checkout or on the VPS.'
echo 'Do not commit the audit folder: it contains private deployment configuration.'
echo "Private audit location: $OUT"
