#!/usr/bin/env bash
# Offline-safe Caddy certificate staging, optional sync for operator-controlled QUIC.
# Not installed or activated by this patch. Never changes Caddy's files.
set -euo pipefail
umask 077

usage() { echo 'Usage: quic-caddy-sync.sh --check|--stage|--sync (requires explicit env, see docs)' >&2; exit 2; }
[[ $# == 1 ]] || usage
mode=$1
[[ $mode == --check || $mode == --stage || $mode == --sync ]] || usage
[[ $EUID == 0 ]] || { echo 'root is required' >&2; exit 1; }
: "${MBOX_CADDY_CERT:?MBOX_CADDY_CERT is required}"
: "${MBOX_CADDY_KEY:?MBOX_CADDY_KEY is required}"
: "${MBOX_QUIC_TLS_DOMAIN:?MBOX_QUIC_TLS_DOMAIN is required}"
store=${MBOX_QUIC_TLS_STORE:-/etc/mbox/quic-caddy}
unit=${MBOX_QUIC_UNIT:-mbox-quic.service}
[[ $store == /* && $store != / && $store != /etc && $store != /etc/mbox ]] || { echo 'unsafe TLS store' >&2; exit 1; }
[[ -f $MBOX_CADDY_CERT && -f $MBOX_CADDY_KEY && ! -L $MBOX_CADDY_CERT && ! -L $MBOX_CADDY_KEY ]] || { echo 'Caddy cert/key must exist as regular, non-symlink files' >&2; exit 1; }

validate_pair() {
  local cert=$1 key=$2 c_hash k_hash
  openssl x509 -in "$cert" -noout -checkend 604800 >/dev/null || { echo 'certificate expires within 7 days' >&2; return 1; }
  openssl x509 -in "$cert" -noout -checkhost "$MBOX_QUIC_TLS_DOMAIN" | grep -q 'does match' || { echo 'certificate does not match DNS name' >&2; return 1; }
  c_hash=$(openssl x509 -in "$cert" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | cut -d' ' -f1)
  k_hash=$(openssl pkey -in "$key" -pubout -outform DER | sha256sum | cut -d' ' -f1)
  [[ $c_hash == "$k_hash" ]] || { echo 'certificate/private key mismatch' >&2; return 1; }
}
validate_pair "$MBOX_CADDY_CERT" "$MBOX_CADDY_KEY"
if [[ $mode == --check ]]; then
  openssl x509 -in "$MBOX_CADDY_CERT" -noout -dates -subject -issuer
  echo 'VALID: source pair and DNS name match; no files modified'
  exit 0
fi

[[ ! -L $store ]] || { echo 'TLS store must not be a symlink' >&2; exit 1; }
install -d -m 0700 -o root -g root "$store/versions"
exec 9>"$store/.sync.lock"
flock -x 9
current=$store/current
if [[ -L $current ]] && cmp -s "$MBOX_CADDY_CERT" "$current/server.crt" && cmp -s "$MBOX_CADDY_KEY" "$current/server.key"; then
  echo 'TLS certificate pair unchanged'
  exit 0
fi
if [[ $mode == --sync ]]; then
  # The operator must first install a verified systemd drop-in explicitly
  # referencing this store, or a first sync could restart old/incorrect TLS.
  systemctl cat "$unit" | grep -F -- "$store/current/server.crt" >/dev/null || {
    echo 'QUIC systemd unit is not configured for synchronized certificate: refusing' >&2; exit 1;
  }
  systemctl cat "$unit" | grep -F -- "$store/current/server.key" >/dev/null || {
    echo 'QUIC systemd unit is not configured for synchronized key: refusing' >&2; exit 1;
  }
fi
stamp=$(date -u +%Y%m%dT%H%M%SZ)
new_dir=$(mktemp -d "$store/versions/$stamp-XXXXXXXX")
trap 'rm -rf -- "$new_dir"' EXIT
install -m 0644 "$MBOX_CADDY_CERT" "$new_dir/server.crt"
install -m 0600 "$MBOX_CADDY_KEY" "$new_dir/server.key"
validate_pair "$new_dir/server.crt" "$new_dir/server.key"
old_target=''
[[ -L $current ]] && old_target=$(readlink "$current")
new_target="versions/$(basename "$new_dir")"
ln -s "$new_target" "$store/.next"
mv -Tf "$store/.next" "$current"
trap - EXIT
if [[ $mode == --stage ]]; then
  echo 'STAGED: cert/key stored; no services or ports changed'
  exit 0
fi
if systemctl restart "$unit" && systemctl is-active --quiet "$unit"; then
  logger -t mbox-quic-caddy-sync 'QUIC restarted with synchronized Caddy TLS credentials; independent client check required'
  echo 'SYNCED: QUIC service restarted; verify client-to-server TLS/Noise health independently'
  exit 0
fi
# Roll back certificate pointer and attempt service recovery, without hiding failure.
echo 'QUIC restart failed; rolling back certificate pointer' >&2
if [[ -n $old_target ]]; then
  ln -s "$old_target" "$store/.rollback"
  mv -Tf "$store/.rollback" "$current"
  systemctl restart "$unit" || true
else
  rm -f -- "$current"
fi
exit 1
