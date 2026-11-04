#!/usr/bin/env bash
# Non-destructive Arch -> root@RU-VPS updater for existing MBox origin.
# No configuration, credentials, key material or REG.RU files are transferred.
set -euo pipefail

usage() {
  echo 'Usage: tools/deploy-origin-root.sh --host root@HOST [--apply]' >&2
  echo 'Default is a read-only preflight. Apply requires a locally compiled dist/vps/mbox-server.' >&2
}
HOST=''
APPLY=false
while (($#)); do
  case "$1" in
    --host) (($# >= 2)) || { usage; exit 2; }; HOST="$2"; shift 2 ;;
    --apply) APPLY=true; shift ;;
    *) usage; exit 2 ;;
  esac
done
[[ "$HOST" =~ ^root@[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || { usage; echo 'Only root@HOST is supported.' >&2; exit 2; }
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER="$ROOT/dist/vps/mbox-server"
CTL="$ROOT/tools/mboxctl/mboxctl.py"
[[ -f "$CTL" ]] || { echo 'mboxctl source missing' >&2; exit 1; }

# This check is always read-only and cannot modify the running service.
ssh -o StrictHostKeyChecking=yes "$HOST" 'set -e; test "$(id -u)" -eq 0; test -x /usr/local/bin/mbox-server; test -x /usr/local/bin/mboxctl; systemctl is-active --quiet mbox-server; echo "RU VPS: existing root-managed origin is active"'
if [[ "$APPLY" != true ]]; then
  echo 'DRY RUN: server left untouched. Build and test on Arch, then rerun with --apply.'
  exit 0
fi
[[ -s "$SERVER" ]] || { echo "Missing compiled origin: $SERVER" >&2; exit 1; }
command -v sha256sum >/dev/null
command -v scp >/dev/null
SERVER_SHA="$(sha256sum "$SERVER" | awk '{print $1}')"
CTL_SHA="$(sha256sum "$CTL" | awk '{print $1}')"
[[ "$SERVER_SHA" =~ ^[0-9a-f]{64}$ && "$CTL_SHA" =~ ^[0-9a-f]{64}$ ]] || exit 1

# A fresh root-owned private staging directory avoids predictable shared /tmp.
REMOTE_STAGE="$(ssh -o StrictHostKeyChecking=yes "$HOST" 'umask 077; mktemp -d /root/mbox-stage.XXXXXXXX')"
[[ "$REMOTE_STAGE" =~ ^/root/mbox-stage\.[A-Za-z0-9]{8,}$ ]] || { echo 'Unexpected remote staging path' >&2; exit 1; }
cleanup() { ssh -o StrictHostKeyChecking=yes "$HOST" "rm -rf -- '$REMOTE_STAGE'" >/dev/null 2>&1 || true; }
trap cleanup EXIT
scp -q -o StrictHostKeyChecking=yes "$SERVER" "$HOST:$REMOTE_STAGE/mbox-server"
scp -q -o StrictHostKeyChecking=yes "$CTL" "$HOST:$REMOTE_STAGE/mboxctl"

# Only the following fixed shell script is executed as root. The positional
# arguments are SHA256 hex and a mktemp-generated safe path, not user input.
ssh -o StrictHostKeyChecking=yes "$HOST" "bash -s -- '$REMOTE_STAGE' '$SERVER_SHA' '$CTL_SHA'" <<'REMOTE'
set -euo pipefail
STAGE="$1" SERVER_SHA="$2" CTL_SHA="$3"
[[ "$STAGE" =~ ^/root/mbox-stage\.[A-Za-z0-9]{8,}$ ]] || exit 1
printf '%s  %s\n' "$SERVER_SHA" "$STAGE/mbox-server" "$CTL_SHA" "$STAGE/mboxctl" | sha256sum -c -
[[ $(id -u) == 0 ]] || exit 1
[[ $(systemctl is-active mbox-server) == active ]] || exit 1
# The backup command includes its isolated test-restore; abort before changing
# binaries if any existing database/key/config cannot be restored safely.
mboxctl backup create
BACKUP="$(umask 077; mktemp -d /root/mbox-rollback.XXXXXXXX)"
cp -p /usr/local/bin/mbox-server "$BACKUP/mbox-server"
cp -p /usr/local/bin/mboxctl "$BACKUP/mboxctl"
COMMITTED=false
rollback() {
  if [[ "$COMMITTED" != true ]]; then
    echo 'Deployment failed: restoring previous root-managed binaries' >&2
    install -m 0755 "$BACKUP/mbox-server" /usr/local/bin/.mbox-server.rollback
    install -m 0755 "$BACKUP/mboxctl" /usr/local/bin/.mboxctl.rollback
    mv -f /usr/local/bin/.mbox-server.rollback /usr/local/bin/mbox-server
    mv -f /usr/local/bin/.mboxctl.rollback /usr/local/bin/mboxctl
    systemctl restart mbox-server || true
  fi
}
trap rollback EXIT
install -m 0755 "$STAGE/mbox-server" /usr/local/bin/.mbox-server.next
install -m 0755 "$STAGE/mboxctl" /usr/local/bin/.mboxctl.next
mv -f /usr/local/bin/.mbox-server.next /usr/local/bin/mbox-server
mv -f /usr/local/bin/.mboxctl.next /usr/local/bin/mboxctl
systemctl restart mbox-server
mboxctl doctor
PID="$(systemctl show --property=MainPID --value mbox-server)"
[[ "$PID" =~ ^[1-9][0-9]*$ ]] || exit 1
[[ "$(sha256sum "/proc/$PID/exe" | cut -d' ' -f1)" == "$SERVER_SHA" ]] || exit 1
[[ "$(sha256sum /usr/local/bin/mbox-server | cut -d' ' -f1)" == "$SERVER_SHA" ]] || exit 1
[[ "$(sha256sum /usr/local/bin/mboxctl | cut -d' ' -f1)" == "$CTL_SHA" ]] || exit 1
COMMITTED=true
trap - EXIT
echo "SUCCESS: running origin matches Arch build; rollback copies remain in $BACKUP"
REMOTE
