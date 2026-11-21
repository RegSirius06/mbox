#!/usr/bin/env bash
# Update only admin code/UI, not CLI, relay or origin. Preserve rollback.
set -euo pipefail
[[ $EUID == 0 ]]
HERE=$(cd "$(dirname "$0")" && pwd)
DEST=/usr/local/lib/mbox-admin
BACKUP=$(mktemp -d /root/mbox-admin-monitor-backup.XXXXXXXX)
python3 -m py_compile "$HERE/mbox_admin.py"
cp -a "$DEST/mbox_admin.py" "$DEST/index.html" "$BACKUP/"
rollback(){ cp -a "$BACKUP/mbox_admin.py" "$BACKUP/index.html" "$DEST/"; systemctl restart mbox-admin.service; }
trap 'rollback' ERR
install -m 0755 "$HERE/mbox_admin.py" "$DEST/mbox_admin.py"
install -m 0644 "$HERE/index.html" "$DEST/index.html"
systemctl restart mbox-admin.service
for i in {1..10}; do
  if [[ $(curl -sS --max-time 3 --unix-socket /run/mbox-admin/admin.sock -o /dev/null -w '%{http_code}' http://localhost/ 2>/dev/null || true) == 200 ]] && [[ $(curl -sS --max-time 3 --unix-socket /run/mbox-admin/admin.sock -o /dev/null -w '%{http_code}' http://localhost/api/monitor 2>/dev/null || true) == 401 ]]; then
    trap - ERR; echo "ADMIN_MONITOR_READY backup=$BACKUP"; exit 0
  fi
  sleep 1
done
false
