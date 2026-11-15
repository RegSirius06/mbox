#!/usr/bin/env bash
# MBox staged installer: explicit production actions only.
# Preserve existing mbox-quic and mbox-quic-staging; no implicit origin restart.
set -euo pipefail
umask 077
ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
CHECK_ONLY=0
UPGRADE_ORIGIN=0
FRESH_INSTALL=0
WITH_QUIC=0
WITH_ADMIN=0
WITH_SUBSCRIPTIONS=0
usage() {
  cat <<'EOF'
Usage: ./install.sh --check|--upgrade-origin|--with-admin|--with-subscriptions|--fresh-install
  --check           Read-only: source and toolchain prerequisites, no changes.
  --upgrade-origin  Explicit upgrade of main origin / CLI (may interrupt sessions).
  --with-admin      Install/update SSH-only admin UI + mboxctl; NO origin restart.
  --with-subscriptions Install/update subscription server + SSH admin UI; NO origin restart.
  --fresh-install   Explicit first installation. Refuses an existing installation.
  --with-quic       Disabled for now: existing 7443/7444 gateways are managed
                    separately, preventing port collisions and trust changes.
  Without a flag:   No changes. Install and upgrade are never implicit.

Do not run --upgrade-origin merely to enable new client-side QUIC TLS support.
EOF
}
for opt in "$@"; do
  case "$opt" in
    --check) CHECK_ONLY=1 ;;
    --upgrade-origin) UPGRADE_ORIGIN=1 ;;
    --fresh-install) FRESH_INSTALL=1 ;;
    --with-quic) WITH_QUIC=1 ;;
    --with-admin) WITH_ADMIN=1 ;;
    --with-subscriptions) WITH_SUBSCRIPTIONS=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $opt" >&2; usage >&2; exit 2 ;;
  esac
done
if (( CHECK_ONLY + UPGRADE_ORIGIN + FRESH_INSTALL + WITH_QUIC + WITH_ADMIN + WITH_SUBSCRIPTIONS > 1 )); then
  echo 'Choose exactly one mode; install operations are deliberately separate.' >&2
  exit 2
fi
if (( WITH_QUIC )); then
  echo 'REFUSED: native QUIC 7443/7444 is managed separately with LoadCredential.' >&2
  echo 'Do not create a competing service or replace TLS while users are connected.' >&2
  exit 2
fi
if (( ! CHECK_ONLY && ! UPGRADE_ORIGIN && ! FRESH_INSTALL && ! WITH_ADMIN && ! WITH_SUBSCRIPTIONS )); then
  echo 'NO OPERATION: production unchanged. Use --check or an explicit mode.'
  exit 0
fi

need() { command -v "$1" >/dev/null 2>&1; }
say() { printf '\n==> %s\n' "$*"; }
for path in server client relay tools/mboxctl/mboxctl.py tools/mbox-key tools/mbox-link native-quic/go.mod deploy/mbox-server.service tools/mbox-admin/mbox_admin.py tools/mbox-admin/config_ui.py tools/mbox-admin/index.html deploy/systemd/mbox-admin.service tools/mbox-subscription/mbox_subscription.py deploy/systemd/mbox-subscription.service tools/quic-cert-renew.sh deploy/systemd/mbox-quic-renew.service deploy/systemd/mbox-quic-renew.timer; do
  [[ -e "$ROOT/$path" ]] || { echo "Missing repository component: $path" >&2; exit 1; }
done

required=(go python3 openssl curl tar systemctl getent groupadd useradd)
missing=()
for c in "${required[@]}"; do need "$c" || missing+=("$c"); done
if (( CHECK_ONLY )); then
  echo "MBox source: $ROOT"
  echo "Version: $(cat "$ROOT/VERSION")"
  if ((${#missing[@]})); then printf 'Missing tools: %s\n' "${missing[*]}"; exit 1; fi
  go version
  (cd "$ROOT/native-quic" && go env GOVERSION GOTOOLCHAIN)
  echo 'CHECK ONLY: no files modified; no processes restarted.'
  exit 0
fi
if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo 'Run installer as root (SSH root on server, or appropriate local elevation)' >&2
  exit 1
fi

# Refuse to repurpose a fresh-install path on a populated production server.
existing=0
[[ -s /etc/mbox/mbox.conf || -s /etc/mbox/server.json ]] && existing=1
if (( UPGRADE_ORIGIN && ! existing )); then
  echo 'REFUSED: no existing MBox config; use --fresh-install instead.' >&2
  exit 1
fi
if (( FRESH_INSTALL && existing )); then
  echo 'REFUSED: existing MBox installation found; use --upgrade-origin explicitly.' >&2
  exit 1
fi
if (( WITH_SUBSCRIPTIONS )); then
  (( existing )) || { echo 'REFUSED: subscription deployment needs existing MBox.' >&2; exit 1; }
  [[ -s /var/lib/mbox/users.db && -s /etc/mbox/mbox.conf ]] || {
    echo 'REFUSED: existing MBox SQLite/config not found.' >&2; exit 1;
  }
  for cmd in python3 systemctl install cp mv; do
    need "$cmd" || { echo "Missing: $cmd" >&2; exit 1; }
  done
  python3 -m py_compile "$ROOT/tools/mbox-subscription/mbox_subscription.py" "$ROOT/tools/mbox-admin/mbox_admin.py" "$ROOT/tools/mbox-admin/config_ui.py"
  # Validate source-generated subscription ingress endpoints before touching services.
  python3 "$ROOT/tools/mbox-subscription/mbox_subscription.py" check
  stamp=$(date -u +%Y%m%dT%H%M%SZ)-$$
  backup="/root/mbox-subscriptions-preinstall-$stamp"
  install -d -m 0700 "$backup"
  for f in /usr/local/bin/mbox-subscription /etc/systemd/system/mbox-subscription.service /etc/systemd/system/mbox-admin.service \
           /usr/local/lib/mbox-admin/mbox_admin.py /usr/local/lib/mbox-admin/config_ui.py /usr/local/lib/mbox-admin/index.html; do
    if [[ -f "$f" ]]; then
      install -d -m 0700 "$backup/$(dirname "${f#/}")"
      cp -p -- "$f" "$backup/${f#/}"
    fi
  done
  # Online SQLite backup; never copy a live WAL database with cp.
  python3 - "$backup/users.db" <<'PYBACKUP'
import sqlite3, sys
source = sqlite3.connect('file:/var/lib/mbox/users.db?mode=ro', uri=True)
dest = sqlite3.connect(sys.argv[1])
try:
    source.backup(dest)
    if dest.execute('PRAGMA integrity_check').fetchone()[0] != 'ok':
        raise RuntimeError('subscription database backup integrity check failed')
finally:
    source.close(); dest.close()
PYBACKUP
  chmod 0600 "$backup/users.db"
  echo "SUBSCRIPTIONS_BACKUP=$backup"
  # Roll back only files touched by this mode. SQLite schema addition is
  # non-destructive; the live DB is not restored automatically as origin may
  # have written user changes since the backup was made.
  rollback_subscriptions() {
    trap - ERR
    systemctl stop mbox-subscription.service >/dev/null 2>&1 || true
    for f in /usr/local/bin/mbox-subscription /etc/systemd/system/mbox-subscription.service /etc/systemd/system/mbox-admin.service \
             /usr/local/lib/mbox-admin/mbox_admin.py /usr/local/lib/mbox-admin/config_ui.py /usr/local/lib/mbox-admin/index.html; do
      if [[ -f "$backup/${f#/}" ]]; then
        cp -p -- "$backup/${f#/}" "$f"
      else
        rm -f -- "$f"
      fi
    done
    systemctl daemon-reload || true
    if [[ -f "$backup/etc/systemd/system/mbox-subscription.service" ]]; then
      systemctl restart mbox-subscription.service || true
    else
      systemctl disable mbox-subscription.service >/dev/null 2>&1 || true
    fi
    systemctl restart mbox-admin.service >/dev/null 2>&1 || true
    echo "SUBSCRIPTIONS ROLLBACK attempted. Files backed up at $backup" >&2
  }
  trap 'rc=$?; rollback_subscriptions; exit "$rc"' ERR
  install -d -m 0755 /usr/local/lib/mbox-admin
  install -d -m 0700 /var/lib/mbox/config-backups
  install -m 0755 "$ROOT/tools/mbox-subscription/mbox_subscription.py" /usr/local/bin/mbox-subscription
  install -m 0644 "$ROOT/deploy/systemd/mbox-subscription.service" /etc/systemd/system/mbox-subscription.service
  install -m 0644 "$ROOT/deploy/systemd/mbox-admin.service" /etc/systemd/system/mbox-admin.service
  install -m 0755 "$ROOT/tools/mbox-admin/mbox_admin.py" /usr/local/lib/mbox-admin/mbox_admin.py
  install -m 0644 "$ROOT/tools/mbox-admin/config_ui.py" /usr/local/lib/mbox-admin/config_ui.py
  install -m 0644 "$ROOT/tools/mbox-admin/index.html" /usr/local/lib/mbox-admin/index.html
  systemctl daemon-reload
  systemctl enable mbox-subscription.service
  systemctl restart mbox-subscription.service
  systemctl is-active --quiet mbox-subscription.service
  if systemctl is-active --quiet mbox-admin.service; then
    systemctl restart mbox-admin.service
    systemctl is-active --quiet mbox-admin.service
  fi
  trap - ERR
  echo 'SUBSCRIPTIONS STAGED: 127.0.0.1:18450 (NOT exposed publicly).'
  echo 'Operator must explicitly add Caddy handle /mbox/sub/* => 127.0.0.1:18450.'
  echo 'No origin/QUIC/Caddy restart. No database users or PSKs changed.'
  exit 0
fi
if (( WITH_ADMIN )); then
  (( existing )) || { echo 'REFUSED: admin UI requires an existing MBox installation.' >&2; exit 1; }
  [[ -s /etc/mbox/mbox.conf ]] || { echo 'REFUSED: admin UI needs current mbox.conf.' >&2; exit 1; }
  [[ -x /usr/local/bin/mbox-server ]] || { echo 'REFUSED: origin binary missing.' >&2; exit 1; }
  for cmd in python3 systemctl install cp mv; do
    need "$cmd" || { echo "Missing admin dependency: $cmd" >&2; exit 1; }
  done
  python3 -m py_compile "$ROOT/tools/mboxctl/mboxctl.py" "$ROOT/tools/mbox-admin/mbox_admin.py" "$ROOT/tools/mbox-admin/config_ui.py"
  # Validate the prospective CLI before installing it, without writing config.
  python3 "$ROOT/tools/mboxctl/mboxctl.py" config validate
  stamp=$(date -u +%Y%m%dT%H%M%SZ)-$$
  backup="/root/mbox-admin-preinstall-$stamp"
  install -d -m 0700 "$backup"
  for f in /usr/local/bin/mboxctl /etc/systemd/system/mbox-admin.service \
           /usr/local/lib/mbox-admin/mbox_admin.py /usr/local/lib/mbox-admin/config_ui.py /usr/local/lib/mbox-admin/index.html; do
    if [[ -f "$f" ]]; then
      install -d -m 0700 "$backup/$(dirname "${f#/}")"
      cp -p -- "$f" "$backup/${f#/}"
    fi
  done
  echo "ADMIN_BACKUP=$backup"
  install -d -m 0755 /usr/local/lib/mbox-admin
  install -d -m 0700 /var/lib/mbox/config-backups
  install -d -m 0700 /etc/mbox-admin
  # Prepare password interactively first. No origin changes even if this fails.
  if [[ ! -f /etc/mbox-admin/password.json ]]; then
    python3 "$ROOT/tools/mbox-admin/mbox_admin.py" init
  fi
  install -m 0755 "$ROOT/tools/mboxctl/mboxctl.py" "/usr/local/bin/.mboxctl.new.$$"
  mv -f -- "/usr/local/bin/.mboxctl.new.$$" /usr/local/bin/mboxctl
  install -m 0755 "$ROOT/tools/mbox-admin/mbox_admin.py" /usr/local/lib/mbox-admin/mbox_admin.py
  install -m 0644 "$ROOT/tools/mbox-admin/config_ui.py" /usr/local/lib/mbox-admin/config_ui.py
  install -m 0644 "$ROOT/tools/mbox-admin/index.html" /usr/local/lib/mbox-admin/index.html
  install -m 0644 "$ROOT/deploy/systemd/mbox-admin.service" /etc/systemd/system/mbox-admin.service
  systemctl daemon-reload
  systemctl enable mbox-admin.service
  systemctl restart mbox-admin.service
  systemctl is-active --quiet mbox-admin.service || {
    echo "Admin service not active. Previous files saved at $backup" >&2; exit 1;
  }
  echo 'ADMIN READY: SSH -N -L 127.0.0.1:8765:/run/mbox-admin/admin.sock root@VPS'
  echo 'Origin, production QUIC, staging QUIC, Caddy and REG.RU were not restarted.'
  exit 0
fi
if ((${#missing[@]})); then
  printf 'Missing tools: %s\n' "${missing[*]}" >&2
  echo 'Install required tools separately; installer will not run apt on production.' >&2
  exit 1
fi
# Go's native-quic module requires Go 1.26+. With GOTOOLCHAIN=auto the go tool
# can obtain the required toolchain. If that fails, do not partially install.
say 'Building all server-side components before modifying running services'
BUILD_DIR="$(mktemp -d /var/tmp/mbox-install.XXXXXXXX)"
trap 'rm -rf -- "$BUILD_DIR"' EXIT
for entry in 'server:mbox-server' 'client:mbox-client' 'tools/mbox-key:mbox-key' 'tools/mbox-link:mbox-link'; do
  dir=${entry%%:*}; name=${entry#*:}
  (cd "$ROOT/$dir" && GO111MODULE=off go build -trimpath -ldflags='-s -w' -o "$BUILD_DIR/$name" .)
done
(cd "$ROOT/relay" && GO111MODULE=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -ldflags='-s -w' -o "$BUILD_DIR/relay.fcgi" .)
(cd "$ROOT/native-quic" && GO111MODULE=on GOTOOLCHAIN=auto \
  go build -trimpath -ldflags='-s -w' -o "$BUILD_DIR/mbox-native-quic" .)
python3 -m py_compile "$ROOT/tools/mboxctl/mboxctl.py"

say 'Creating pre-change backup (SQLite online snapshot + file archive)'
STAMP=$(date -u +%Y%m%dT%H%M%SZ)
BACKUP="/root/mbox-preinstall-${STAMP}"
mkdir -m 0700 "$BACKUP"
# A live SQLite database in WAL mode must not be backed up using plain tar.
if [[ -f /var/lib/mbox/users.db ]]; then
  python3 - /var/lib/mbox/users.db "$BACKUP/users.db" <<'PY'
import sqlite3,sys
src=sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True,timeout=10)
dst=sqlite3.connect(sys.argv[2])
try:
    src.backup(dst)
    assert dst.execute('PRAGMA integrity_check').fetchone()[0]=='ok'
finally:
    dst.close();src.close()
PY
  chmod 0600 "$BACKUP/users.db"
fi
items=()
for p in /etc/mbox /var/lib/mbox /etc/systemd/system/mbox-server.service \
   /etc/systemd/system/mbox-native-quic.service /etc/systemd/system/mbox-admin.service \
   /etc/systemd/system/mbox-quic.service /etc/systemd/system/mbox-quic-staging.service \
   /etc/systemd/system/mbox-quic-caddy-sync.service /etc/systemd/system/mbox-quic-caddy-sync.timer \
   /usr/local/lib/mbox-admin /etc/mbox-admin \
   /etc/systemd/system/mbox-quic-renew.service /etc/systemd/system/mbox-quic-renew.timer \
   /usr/local/libexec/mbox-quic-cert-renew \
   /usr/local/bin/mbox-server /usr/local/bin/mbox-client /usr/local/bin/mboxctl \
   /usr/local/bin/mbox-key /usr/local/bin/mbox-link /usr/local/bin/mbox-native-quic \
   /usr/local/bin/mbox-native-quic-staging /usr/local/sbin/mbox-quic-caddy-sync \
   /etc/mbox/quic-caddy /usr/local/lib/mbox /usr/local/share/mbox/regru-site; do
  [[ -e "$p" ]] && items+=("${p#/}")
done
if ((${#items[@]})); then
  tar -C / --exclude='var/lib/mbox/users.db' --exclude='var/lib/mbox/users.db-wal' \
    --exclude='var/lib/mbox/users.db-shm' -czf "$BACKUP/files.tar.gz" "${items[@]}"
else
  tar -czf "$BACKUP/files.tar.gz" --files-from /dev/null
fi
chmod 0600 "$BACKUP/files.tar.gz"
[[ -s "$BACKUP/files.tar.gz" ]] || { echo 'Backup missing files archive' >&2; exit 1; }
if [[ -f /var/lib/mbox/users.db ]]; then
  [[ -s "$BACKUP/users.db" ]] || { echo 'Backup missing SQLite snapshot' >&2; exit 1; }
fi
echo "BACKUP=$BACKUP (rollback is manual, no automatic DB overwrite)"

say 'Creating unprivileged service account'
if ! getent group mbox >/dev/null 2>&1; then groupadd --system mbox; fi
if ! id -u mbox >/dev/null 2>&1; then useradd --system --gid mbox --home-dir /nonexistent --shell /usr/sbin/nologin mbox; fi

say 'Installing origin binaries (explicit operator-approved action)'
install -d -m 0755 /usr/local/lib/mbox /usr/local/share/mbox/regru-site
for n in mbox-server mbox-client mbox-key mbox-link mbox-native-quic; do
  install -m 0755 "$BUILD_DIR/$n" "/usr/local/bin/.$n.new.$$"
  mv -f -- "/usr/local/bin/.$n.new.$$" "/usr/local/bin/$n"
done
install -m 0755 "$ROOT/tools/mboxctl/mboxctl.py" "/usr/local/bin/.mboxctl.new.$$"
mv -f -- "/usr/local/bin/.mboxctl.new.$$" /usr/local/bin/mboxctl
install -m 0755 "$BUILD_DIR/relay.fcgi" /usr/local/lib/mbox/relay.fcgi
install -m 0644 "$ROOT/deploy/regru/site/index.html" /usr/local/share/mbox/regru-site/index.html
install -m 0644 "$ROOT/deploy/regru/site/404.html" /usr/local/share/mbox/regru-site/404.html
install -m 0644 "$ROOT/deploy/mbox-server.service" /etc/systemd/system/mbox-server.service
# Stage renewal tooling, but never enable it without an authenticated CA migration.
install -d -m 0755 /usr/local/libexec
install -m 0755 "$ROOT/tools/quic-cert-renew.sh" /usr/local/libexec/mbox-quic-cert-renew
install -m 0644 "$ROOT/deploy/systemd/mbox-quic-renew.service" /etc/systemd/system/mbox-quic-renew.service
install -m 0644 "$ROOT/deploy/systemd/mbox-quic-renew.timer" /etc/systemd/system/mbox-quic-renew.timer

systemctl daemon-reload

say 'Upgrading existing config or initializing new deployment'
if [[ -s /etc/mbox/mbox.conf ]]; then
  mboxctl config upgrade
  mboxctl config validate
  mboxctl relay bundle >/dev/null
elif [[ -s /etc/mbox/server.json ]]; then
  mboxctl migrate legacy --server-json /etc/mbox/server.json
else
  echo 'Fresh install: configure public HTTPS frontend, origin URL and first user.'
  mboxctl init
fi
mboxctl sync >/dev/null
systemctl enable mbox-server.service >/dev/null
systemctl restart mbox-server.service
systemctl is-active --quiet mbox-server.service || {
  echo "Origin service failed. Previous state files are in $BACKUP; inspect before manual rollback." >&2
  exit 1
}
say 'Self-check'
mboxctl doctor
echo 'Origin installed; existing QUIC services were not modified.'
echo 'REG.RU FastCGI and Caddy/sing-box require separate operator deployment.'
echo 'TLS renewal is NOT auto-enabled; existing QUIC services remain untouched.'
echo "Backup: $BACKUP"
