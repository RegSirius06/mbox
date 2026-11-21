#!/usr/bin/env bash
# Install from the current source checkout without restarting origin/QUIC.
set -euo pipefail
umask 077
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
(( EUID == 0 )) || { echo 'Run on the origin VPS as root.' >&2; exit 1; }
[[ -s /etc/mbox/mbox.conf && -s /etc/mbox/server.json ]] || { echo 'Existing MBox installation required' >&2; exit 1; }
version=$(cat "$ROOT/VERSION")
grep -Fq "mboxctl $version" "$ROOT/tools/mboxctl/mboxctl.py" || { echo 'CLI version mismatch' >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
bash "$ROOT/tools/build-relay.sh" "$work/relay.fcgi"
python3 -m py_compile "$ROOT/tools/mboxctl/mboxctl.py"
install -d -m 0755 /usr/local/lib/mbox /usr/local/bin
backup=$(mktemp -d /root/mbox-relay-local-backup.XXXXXXXX)
for p in /usr/local/bin/mboxctl /usr/local/lib/mbox/relay.fcgi; do
    [[ ! -L "$p" ]] || { echo "Refusing symlink: $p" >&2; exit 1; }
    if [[ -f "$p" ]]; then cp -p -- "$p" "$backup/$(basename "$p")"; fi
done
install -m 0755 "$work/relay.fcgi" /usr/local/lib/mbox/.relay.fcgi.new.$$
install -m 0755 "$ROOT/tools/mboxctl/mboxctl.py" /usr/local/bin/.mboxctl.new.$$
mv -f -- /usr/local/lib/mbox/.relay.fcgi.new.$$ /usr/local/lib/mbox/relay.fcgi
mv -f -- /usr/local/bin/.mboxctl.new.$$ /usr/local/bin/mboxctl
printf 'LOCAL_BACKUP=%s\n' "$backup"
sha256sum /usr/local/lib/mbox/relay.fcgi
echo 'RELAY_LOCAL_INSTALL_OK (no remote deploy; no service restart)'
