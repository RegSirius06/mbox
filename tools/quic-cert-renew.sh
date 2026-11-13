#!/usr/bin/env bash
# OPTIONAL operator-installed unattended QUIC server certificate rotation.
# CA private key stored on VPS = critical trust compromise if VPS is compromised.
# Not enabled or deployed by applying the source overlay.
set -euo pipefail
umask 077

[[ $EUID -eq 0 ]] || { echo 'root required' >&2; exit 1; }
: "${MBOX_QUIC_ENDPOINT_IP:?set MBOX_QUIC_ENDPOINT_IP to the numeric certificate IP SAN}"
pki=${MBOX_QUIC_PKI_DIR:-/etc/mbox/quic}
unit=${MBOX_QUIC_UNIT:-mbox-native-quic.service}
group=${MBOX_QUIC_READ_GROUP:-mbox}
renew_before_days=${MBOX_QUIC_RENEW_BEFORE_DAYS:-14}
valid_days=${MBOX_QUIC_LEAF_DAYS:-60}
[[ $renew_before_days =~ ^[0-9]+$ && $valid_days =~ ^[0-9]+$ ]] || exit 2
(( renew_before_days >= 1 && valid_days >= 30 && valid_days <= 90 )) || exit 2
python3 - "$MBOX_QUIC_ENDPOINT_IP" <<'PY'
import ipaddress, sys
try:
    # beta.4 Android profiles intentionally require numerical IPv4.
    assert isinstance(ipaddress.ip_address(sys.argv[1]), ipaddress.IPv4Address)
except (ValueError, AssertionError):
    sys.exit('only numeric IPv4 endpoints are supported in this rotation tool')
PY

[[ -d $pki && ! -L $pki ]] || { echo 'PKI directory must exist and not be symlink' >&2; exit 1; }
ca_cert=$pki/ca.crt
ca_key=$pki/ca.key
[[ -f $ca_cert && -f $ca_key && ! -L $ca_key && ! -L $ca_cert ]] || { echo 'CA certificate and key must exist as regular files' >&2; exit 1; }
[[ $(stat -c %a "$ca_key") == 600 ]] || { echo 'CA key must have mode 0600' >&2; exit 1; }
install -d -m 0750 -o root -g "$group" "$pki/versions"
exec 9>"$pki/.renew.lock"
flock -n 9 || exit 0

current=$pki/current
if [[ -L $current ]] && \
   openssl verify -CAfile "$ca_cert" -verify_ip "$MBOX_QUIC_ENDPOINT_IP" "$current/server.crt" >/dev/null 2>&1 && \
   openssl x509 -in "$current/server.crt" -noout -checkend "$(( renew_before_days * 86400 ))" >/dev/null 2>&1; then
    echo 'QUIC TLS renewal not yet required'
    exit 0
fi

stamp=$(date -u +%Y%m%dT%H%M%SZ)
name="$stamp-$(openssl rand -hex 5)"
staging="$pki/versions/$name"
install -d -m 0750 -o root -g "$group" "$staging"
rollback() { rm -rf -- "$staging"; }
trap rollback EXIT

openssl ecparam -name prime256v1 -genkey -noout -out "$staging/server.key"
openssl req -new -sha256 -key "$staging/server.key" -out "$staging/request.csr" \
  -subj '/CN=MBox QUIC Server'
cat > "$staging/extensions.txt" <<EXT
basicConstraints=critical,CA:FALSE
keyUsage=critical,digitalSignature
extendedKeyUsage=serverAuth
subjectAltName=IP:$MBOX_QUIC_ENDPOINT_IP
EXT
openssl x509 -req -in "$staging/request.csr" -CA "$ca_cert" -CAkey "$ca_key" \
  -set_serial "0x$(openssl rand -hex 16)" -days "$valid_days" -sha256 \
  -extfile "$staging/extensions.txt" -out "$staging/server.crt"
openssl verify -CAfile "$ca_cert" -verify_ip "$MBOX_QUIC_ENDPOINT_IP" "$staging/server.crt"
openssl x509 -in "$staging/server.crt" -noout -checkend "$(( renew_before_days * 86400 ))" >/dev/null
openssl pkey -in "$staging/server.key" -pubout -outform DER | sha256sum | cut -d' ' -f1 > "$staging/key.sha"
openssl x509 -in "$staging/server.crt" -pubkey -noout | openssl pkey -pubin -outform DER | sha256sum | cut -d' ' -f1 > "$staging/cert.sha"
cmp -s "$staging/key.sha" "$staging/cert.sha" || { echo 'TLS key/certificate mismatch' >&2; exit 1; }
rm -f -- "$staging/request.csr" "$staging/extensions.txt" "$staging/key.sha" "$staging/cert.sha"
chown root:"$group" "$staging/server.key" "$staging/server.crt"
chmod 0640 "$staging/server.key"
chmod 0644 "$staging/server.crt"

old_target=''
if [[ -L $current ]]; then old_target=$(readlink "$current"); fi
new_target="versions/$name"
ln -s "$new_target" "$pki/.current.next"
mv -Tf "$pki/.current.next" "$current"
if ! systemctl restart "$unit" || ! systemctl is-active --quiet "$unit"; then
    echo 'QUIC gateway restart failed; rolling back TLS current symlink' >&2
    if [[ -n $old_target ]]; then
        ln -s "$old_target" "$pki/.current.rollback"
        mv -Tf "$pki/.current.rollback" "$current"
        systemctl restart "$unit" || true
    else
        rm -f -- "$current"
    fi
    exit 1
fi
trap - EXIT
logger -t mbox-quic-renew 'QUIC leaf certificate renewed; check logs and health manually'
echo 'QUIC TLS renewed; CA unchanged; existing CA-trusting profiles remain valid'
