#!/usr/bin/env bash
set -euo pipefail

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "Run: sudo ./install.sh" >&2
  exit 1
fi

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TS=$(date +%Y%m%d-%H%M%S)
BACKUP="/root/mbox-preinstall-${TS}.tar.gz"

say() { printf '\n==> %s\n' "$*"; }
need() { command -v "$1" >/dev/null 2>&1; }

say "Checking dependencies"
missing=()
for c in go python3 openssl curl scp; do
  need "$c" || missing+=("$c")
done
if ((${#missing[@]})); then
  if need apt-get; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y golang-go python3 openssl curl openssh-client qrencode ca-certificates
  else
    echo "Missing commands: ${missing[*]}" >&2
    echo "Install Go, Python 3, OpenSSL, curl and OpenSSH client, then rerun install.sh." >&2
    exit 1
  fi
fi

say "Saving rollback archive"
items=()
for p in /etc/mbox /var/lib/mbox /etc/systemd/system/mbox-server.service \
         /usr/local/bin/mbox-server /usr/local/bin/mbox-client \
         /usr/local/bin/mboxctl /usr/local/bin/mbox-key /usr/local/bin/mbox-link \
         /usr/local/lib/mbox /usr/local/share/mbox/regru-site; do
  [[ -e "$p" ]] && items+=("${p#/}")
done
if ((${#items[@]})); then
  tar -C / -czf "$BACKUP" "${items[@]}"
else
  tar -czf "$BACKUP" --files-from /dev/null
fi
chmod 600 "$BACKUP"
echo "Rollback: $BACKUP"

say "Building MBox"
(
  cd "$ROOT/server"
  GO111MODULE=off go build -trimpath -ldflags='-s -w' -o /tmp/mbox-server *.go
)
(
  cd "$ROOT/client"
  GO111MODULE=off go build -trimpath -ldflags='-s -w' -o /tmp/mbox-client *.go
)
(
  cd "$ROOT/tools/mbox-key"
  GO111MODULE=off go build -trimpath -ldflags='-s -w' -o /tmp/mbox-key *.go
)
(
  cd "$ROOT/tools/mbox-link"
  GO111MODULE=off go build -trimpath -ldflags='-s -w' -o /tmp/mbox-link *.go
)
(
  cd "$ROOT/relay"
  GO111MODULE=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /tmp/relay.fcgi *.go
)
python3 -m py_compile "$ROOT/tools/mboxctl/mboxctl.py"

say "Creating unprivileged service account"
if ! getent group mbox >/dev/null 2>&1; then groupadd --system mbox; fi
if ! id -u mbox >/dev/null 2>&1; then useradd --system --gid mbox --home-dir /nonexistent --shell /usr/sbin/nologin mbox; fi

say "Installing binaries"
install -d -m 0755 /usr/local/lib/mbox
install -d -m 0755 /usr/local/share/mbox/regru-site
install -m 0755 /tmp/mbox-server /usr/local/bin/mbox-server
install -m 0755 /tmp/mbox-client /usr/local/bin/mbox-client
install -m 0755 /tmp/mbox-key /usr/local/bin/mbox-key
install -m 0755 /tmp/mbox-link /usr/local/bin/mbox-link
install -m 0755 "$ROOT/tools/mboxctl/mboxctl.py" /usr/local/bin/mboxctl
install -m 0755 /tmp/relay.fcgi /usr/local/lib/mbox/relay.fcgi
install -m 0644 "$ROOT/deploy/regru/site/index.html" /usr/local/share/mbox/regru-site/index.html
install -m 0644 "$ROOT/deploy/regru/site/404.html" /usr/local/share/mbox/regru-site/404.html
install -m 0644 "$ROOT/deploy/mbox-server.service" /etc/systemd/system/mbox-server.service
systemctl daemon-reload

say "Configuring"
if [[ -s /etc/mbox/mbox.conf ]]; then
  echo "Existing new-style configuration found."
  mboxctl config upgrade
  mboxctl sync --restart
  mboxctl relay bundle >/dev/null
  systemctl enable --now mbox-server.service
elif [[ -s /etc/mbox/server.json ]]; then
  echo "Legacy MBox configuration found; importing it into SQLite/mbox.conf."
  mboxctl migrate legacy --server-json /etc/mbox/server.json
else
  echo "Fresh installation. The setup wizard will ask only deployment-specific values."
  mboxctl init
fi

# Whatever branch ran above, make sure the process in memory is the binary
# that was just installed.  This is essential when migrating old production:
# the old daemon may not implement MBox's SIGHUP auth reload semantics.
mboxctl sync >/dev/null
systemctl daemon-reload
systemctl enable mbox-server.service >/dev/null
systemctl restart mbox-server.service
sleep 0.2
systemctl is-active --quiet mbox-server.service || {
  systemctl status mbox-server.service --no-pager -l || true
  echo "mbox-server failed to start" >&2
  exit 1
}

say "Self-check"
mboxctl doctor

echo
echo "Installation complete."
echo "Admin CLI:        /usr/local/bin/mboxctl"
echo "Admin config:     /etc/mbox/mbox.conf"
echo "Runtime config:   /etc/mbox/server.json"
echo "Users database:   /var/lib/mbox/users.db"
echo "REG.RU artifacts: /var/lib/mbox/regru/current/"
echo "REG.RU bundle:    /var/lib/mbox/regru/mbox-regru-bundle.tar.gz"
echo "Rollback archive: $BACKUP"
