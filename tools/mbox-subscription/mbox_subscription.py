#!/usr/bin/env python3
"""MBox subscription gateway. Stdlib only. Never exposes the admin interface.

Public GET /mbox/sub/<random-token> returns MBox direct profiles from mboxctl
for configured ingress relays. Tokens are stored only as SHA-256 digests.
"""
import argparse
import base64
import configparser
from datetime import datetime, timezone
import hashlib
import grp
import hmac
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import threading
import importlib.util
from importlib.machinery import SourceFileLoader
import json
import os
from pathlib import Path
import re
import secrets
import sqlite3
import sys
import time
from urllib.parse import urlsplit

DB = Path(os.environ.get('MBOX_STATE', '/var/lib/mbox')) / 'users.db'
CONF = Path(os.environ.get('MBOX_ETC', '/etc/mbox')) / 'mbox.conf'
EGRESS = Path(os.environ.get('MBOX_EGRESS', str(CONF.parent / 'egress.json')))
DEFAULT_EXIT_LABEL = Path(os.environ.get('MBOX_SUB_DEFAULT_EXIT_LABEL', str(CONF.parent / 'subscription-default-exit-label')))
INGRESS = Path(os.environ.get('MBOX_SUB_ENDPOINTS', '/etc/mbox/subscription-endpoints.json'))
CTL = Path(os.environ.get('MBOX_SUB_CTL', '/usr/local/bin/mboxctl'))
LINK_KEY = Path(os.environ.get('MBOX_SUB_LINK_KEY', '/etc/mbox/subscription-link.key'))
PREFIX = '/mbox/sub/'
UID = re.compile(r'[A-Za-z0-9._-]{1,63}\Z')
ENDPOINT_ID = re.compile(r'[A-Za-z0-9_-]{1,32}\Z')
TOKEN = re.compile(r'[A-Za-z0-9_-]{42,48}\Z')
MAX_PROFILES = 12


def fail(message):
    raise ValueError(message)


def checked_uid(uid):
    if not isinstance(uid, str) or not UID.fullmatch(uid) or uid.startswith('-'):
        fail('invalid user id')
    return uid


def checked_url(value):
    if not isinstance(value, str) or len(value) > 512 or not value.startswith('https://'):
        fail('ingress URL must be HTTPS')
    u = urlsplit(value)
    if not u.hostname or u.username or u.password or u.query or u.fragment or u.path.endswith('/') or not u.netloc:
        fail('ingress URL must be HTTPS with no trailing slash, query or credentials')
    try:
        port = u.port
    except ValueError:
        fail('invalid ingress port')
    if port is not None and not 1 <= port <= 65535:
        fail('invalid ingress port')
    if len(value.encode('utf-8')) > 512:
        fail('ingress URL too long')
    return value


def db(open_only=False):
    if open_only:
        con = sqlite3.connect(DB.as_uri() + '?mode=ro', uri=True, timeout=3)
    else:
        con = sqlite3.connect(DB, timeout=3)
    con.row_factory = sqlite3.Row
    con.execute('PRAGMA busy_timeout=3000')
    return con


def schema(con):
    # Separate table; never modifies an existing users row or PSK.
    con.execute('''CREATE TABLE IF NOT EXISTS subscription_tokens (
       digest TEXT PRIMARY KEY CHECK(length(digest)=64),
       user_id TEXT NOT NULL,
       created_at INTEGER NOT NULL,
       revoked_at INTEGER
    )''')
    con.execute('CREATE INDEX IF NOT EXISTS idx_sub_user ON subscription_tokens(user_id)')
    # Only a random nonce is retained; the bearer token is re-derived with
    # a dedicated root-owned HMAC key (never stored as plaintext in SQLite).
    con.execute('''CREATE TABLE IF NOT EXISTS subscription_primary (
        user_id TEXT PRIMARY KEY, nonce BLOB NOT NULL CHECK(length(nonce)=16), created_at INTEGER NOT NULL
    )''')
    # Direct mboxctl deletions must also revoke old links. Prevent future users
    # with recycled IDs from inheriting deleted users' subscriptions.
    con.execute('''CREATE TRIGGER IF NOT EXISTS subscription_tokens_on_user_delete
        AFTER DELETE ON users BEGIN
            UPDATE subscription_tokens SET revoked_at=CAST(strftime('%s','now') AS INTEGER)
                WHERE user_id=OLD.id AND revoked_at IS NULL;
            DELETE FROM subscription_primary WHERE user_id=OLD.id;
        END''')



def link_key(allow_create=False):
    # Existing canonical subscriptions must NEVER be silently rekeyed.
    # A missing key after restore is a recoverable administrative error,
    # not a reason to invalidate all already issued links.
    if not LINK_KEY.exists():
        if not allow_create:
            fail('subscription signing key is missing; restore subscription-link.key from backup')
        if not LINK_KEY.parent.is_dir() or LINK_KEY.parent.is_symlink():
            fail('subscription key directory is unsafe')
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW
        try:
            fd = os.open(LINK_KEY, flags, 0o600)
        except FileExistsError:
            pass
        else:
            with os.fdopen(fd, 'wb') as f:
                f.write(secrets.token_bytes(32))
                f.flush()
                os.fsync(f.fileno())
    st = LINK_KEY.lstat()
    if not LINK_KEY.is_file() or LINK_KEY.is_symlink() or st.st_mode & 0o077 or st.st_size != 32:
        fail('subscription signing key must be a real 0600 file of 32 bytes')
    key = LINK_KEY.read_bytes()
    if len(key) != 32:
        fail('invalid subscription signing key')
    return key


def primary_token(uid, nonce, key):
    if not isinstance(nonce, bytes) or len(nonce) != 16 or len(key) != 32:
        fail('invalid canonical subscription data')
    material = b'MBox canonical subscription v1\0' + uid.encode('ascii') + b'\0' + nonce
    return base64.urlsafe_b64encode(hmac.new(key, material, hashlib.sha256).digest()).decode('ascii').rstrip('=')


def get_or_create_subscription(uid):
    checked_uid(uid)
    url = public_url()
    with db() as con:
        # Serialize clicks and revocations, so two parallel requests can
        # never create two canonical tokens for the same user.
        con.execute('BEGIN IMMEDIATE')
        schema(con)
        row = con.execute('SELECT id, enabled FROM users WHERE id=?', (uid,)).fetchone()
        if not row or not row['enabled']:
            fail('unknown or disabled user')
        existing = con.execute('SELECT nonce FROM subscription_primary WHERE user_id=?', (uid,)).fetchone()
        if existing:
            key = link_key(allow_create=False)
            nonce = bytes(existing['nonce'])
            token = primary_token(uid, nonce, key)
            digest = hashlib.sha256(token.encode('ascii')).hexdigest()
            active = con.execute('SELECT 1 FROM subscription_tokens '
                'WHERE digest=? AND user_id=? AND revoked_at IS NULL', (digest, uid)).fetchone()
            if not active:
                fail('canonical subscription inconsistent; do not silently rotate it')
        else:
            # There may be legacy token digests already in this database.
            # They remain valid, but cannot be recovered for display.
            has_primary = con.execute('SELECT 1 FROM subscription_primary LIMIT 1').fetchone()
            key = link_key(allow_create=not bool(has_primary))
            nonce = secrets.token_bytes(16)
            token = primary_token(uid, nonce, key)
            digest = hashlib.sha256(token.encode('ascii')).hexdigest()
            stamp = int(time.time())
            con.execute('INSERT INTO subscription_tokens(digest,user_id,created_at) VALUES(?,?,?)',
                        (digest, uid, stamp))
            con.execute('INSERT INTO subscription_primary(user_id,nonce,created_at) VALUES(?,?,?)',
                        (uid, nonce, stamp))
    return url + '/' + token


def revoke_subscriptions(uid):
    checked_uid(uid)
    with db() as con:
        con.execute('BEGIN IMMEDIATE')
        schema(con)
        count = con.execute('UPDATE subscription_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL',
                            (int(time.time()), uid)).rowcount
        con.execute('DELETE FROM subscription_primary WHERE user_id=?', (uid,))
    return count

def public_url():
    cp = configparser.ConfigParser(interpolation=None)
    if not cp.read(CONF):
        fail('mbox.conf not found')
    specified = cp.get('subscription', 'public_url', fallback='').strip()
    if not specified:
        # Prefer the VPS's TLS domain over the REG.RU shared-host ingress.
        name = cp.get('quic', 'server_name', fallback='').strip()
        if name and re.fullmatch(r'[A-Za-z0-9.-]{1,253}', name):
            specified = 'https://' + name + PREFIX.rstrip('/')
        else:
            root = urlsplit(cp['server']['public_url'])
            if root.scheme != 'https' or not root.hostname:
                fail('cannot derive HTTPS subscription domain')
            specified = 'https://' + root.netloc + PREFIX.rstrip('/')
    url = checked_url(specified)
    # The public URL is an administrator-selected Caddy route. Internally the
    # HTTP service still receives PREFIX via Caddy's single rewrite rule.
    parsed = urlsplit(url)
    domain = cp.get('quic', 'server_name', fallback='').strip().lower()
    if (not domain or parsed.hostname != domain or parsed.port not in (None, 443)
            or parsed.username is not None or parsed.password is not None
            or parsed.query or parsed.fragment
            or not re.fullmatch(r'(?:/[A-Za-z0-9_-]{1,32}){1,4}', parsed.path)):
        fail('subscription.public_url must be https://<quic.server_name>/<safe-path>')
    return url


def configured_endpoints():
    cp = configparser.ConfigParser(interpolation=None)
    if not cp.read(CONF):
        fail('mbox.conf not found')
    primary = checked_url(cp['server']['public_url'])
    primary = {'id': 'primary', 'name': 'Primary', 'url': primary}
    if not INGRESS.exists():
        return [primary]
    if INGRESS.is_symlink() or INGRESS.stat().st_size > 8192:
        fail('invalid endpoints file')
    v = json.loads(INGRESS.read_text(encoding='utf-8'))
    if not isinstance(v, dict) or not isinstance(v.get('endpoints'), list):
        fail('endpoints must be a JSON object with endpoints array')
    seen = set()
    entries = []
    for row in v['endpoints']:
        if not isinstance(row, dict):
            fail('invalid ingress record')
        ident, name, url = row.get('id'), row.get('name'), row.get('url')
        if not isinstance(ident, str) or not ENDPOINT_ID.fullmatch(ident) or ident in seen:
            fail('invalid/duplicate ingress ID')
        if not isinstance(name, str) or not 1 <= len(name) <= 48 or '\n' in name:
            fail('invalid ingress label')
        seen.add(ident)
        entries.append({'id': ident, 'name': name, 'url': checked_url(url)})
    if not 1 <= len(entries) <= MAX_PROFILES:
        fail('configure 1..12 ingress endpoints')
    return entries


def write_endpoints(items):
    if not INGRESS.parent.is_dir() or INGRESS.is_symlink():
        fail('endpoints directory absent or file is symlink')
    # Validate the candidate fully before changing the file.
    if not 1 <= len(items) <= MAX_PROFILES:
        fail('at least 1 and at most 12 ingress endpoints required')
    for x in items:
        if not ENDPOINT_ID.fullmatch(x['id']) or not 1 <= len(x['name']) <= 48:
            fail('invalid ingress')
        checked_url(x['url'])
    payload = (json.dumps({'endpoints': items}, ensure_ascii=False, indent=2) + '\n').encode('utf-8')
    tmp = INGRESS.with_name('.subscription-endpoints.' + str(os.getpid()) + '.tmp')
    fd = os.open(tmp, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, 'wb') as f:
            f.write(payload)
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp, INGRESS)
    finally:
        tmp.unlink(missing_ok=True)


# Same allowlist as the origin. Egresses are local SOCKS5 listeners, never
# arbitrary user-supplied IP addresses or a second FastCGI ingress.
def valid_egress(entry):
    if not isinstance(entry, dict) or set(entry) != {'id','name','upstream'}:
        fail('invalid egress entry')
    ident, name, upstream = entry['id'], entry['name'], entry['upstream']
    if not isinstance(ident, str) or not re.fullmatch(r'[a-z][a-z0-9_-]{0,15}', ident):
        fail('invalid exit ID')
    if not isinstance(name, str) or not 1 <= len(name) <= 48 or any(c in name for c in '\r\n\t'):
        fail('invalid exit name')
    if not isinstance(upstream, str) or len(upstream) > 64:
        fail('invalid exit upstream')
    from ipaddress import ip_address
    import socket
    try:
        host,port = (upstream[1:].split(']:',1) if upstream.startswith('[') else upstream.rsplit(':',1))
        ip = ip_address(host)
        if not ip.is_loopback or not port.isdecimal() or not 1 <= int(port) <= 65535:
            fail('upstream must be loopback SOCKS5 IP:port')
    except (ValueError, TypeError, IndexError):
        fail('upstream must be loopback SOCKS5 IP:port')
    return {'id':ident,'name':name,'upstream':upstream}


def configured_exits():
    if not EGRESS.exists():
        return []
    if EGRESS.is_symlink() or EGRESS.stat().st_size > 8192 or not EGRESS.is_file():
        fail('invalid egress file')
    obj = json.loads(EGRESS.read_text(encoding='utf-8'))
    if not isinstance(obj, dict) or set(obj) != {'exits'} or not isinstance(obj['exits'],list) or len(obj['exits']) > 12:
        fail('invalid egress file')
    exits = [valid_egress(row) for row in obj['exits']]
    if len({item['id'] for item in exits}) != len(exits):
        fail('duplicate egress ID')
    return exits


def write_exits(rows):
    if not EGRESS.parent.is_dir() or EGRESS.is_symlink() or len(rows) > 12:
        fail('unsafe egress location')
    checked=[valid_egress(row) for row in rows]
    if len({row['id'] for row in checked}) != len(checked):
        fail('duplicate exit ID')
    data=(json.dumps({'exits':checked},indent=2,ensure_ascii=False)+'\n').encode('utf-8')
    tmp=EGRESS.with_name('.egress-'+str(os.getpid())+'-'+secrets.token_hex(4)+'.tmp')
    fd=os.open(tmp,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    try:
        with os.fdopen(fd,'wb') as f:
            # Admin runs as root with UMask=0077. Keep each atomic
            # replacement readable by the unprivileged origin user.
            if EGRESS == Path('/etc/mbox/egress.json'):
                os.fchown(f.fileno(), -1, grp.getgrnam('mbox').gr_gid)
                os.fchmod(f.fileno(), 0o640)
            f.write(data); f.flush(); os.fsync(f.fileno())
        os.replace(tmp,EGRESS)
    finally:
        tmp.unlink(missing_ok=True)


def validate_default_exit_label(label):
    if not isinstance(label, str) or not 1 <= len(label) <= 48 or any(ord(c) < 32 or ord(c) == 127 for c in label):
        fail('invalid default exit label')
    return label


def default_exit_label():
    if not DEFAULT_EXIT_LABEL.exists() and not DEFAULT_EXIT_LABEL.is_symlink():
        return 'Default'
    if DEFAULT_EXIT_LABEL.is_symlink() or not DEFAULT_EXIT_LABEL.is_file() or DEFAULT_EXIT_LABEL.stat().st_size > 192:
        fail('invalid default exit label file')
    return validate_default_exit_label(DEFAULT_EXIT_LABEL.read_text(encoding='utf-8').removesuffix('\n'))


def write_default_exit_label(label):
    label = validate_default_exit_label(label)
    if not DEFAULT_EXIT_LABEL.parent.is_dir() or DEFAULT_EXIT_LABEL.parent.is_symlink() or DEFAULT_EXIT_LABEL.is_symlink():
        fail('unsafe default exit label location')
    temp = DEFAULT_EXIT_LABEL.with_name('.subscription-default-exit-' + secrets.token_hex(6) + '.tmp')
    fd = os.open(temp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as stream:
            stream.write(label + '\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temp, DEFAULT_EXIT_LABEL)
    finally:
        temp.unlink(missing_ok=True)


def ctl_module():
    # Reuse the authoritative beta.4.2 profile generator; no second wire codec.
    loader = SourceFileLoader('_mbox_subscription_ctl', str(CTL))
    spec = importlib.util.spec_from_loader(loader.name, loader)
    mod = importlib.util.module_from_spec(spec)
    loader.exec_module(mod)
    # mboxctl's generic db_connect() creates/chmods directories because it is
    # also used by root administration commands. A public subscription GET is
    # strictly read-only: keep the canonical serializer but replace only this
    # module instance's DB accessor with an immutable SQLite connection.
    mod.db_connect = lambda: db(open_only=True)
    return mod


def profile_link(ctl, uid, entry, exit_row=None, display_name=None):
    link = ctl.build_link(uid)
    if not link.startswith('mbox://'):
        fail('mboxctl returned an invalid link')
    token = link[7:].split('#', 1)[0]
    raw = base64.urlsafe_b64decode(token + '=' * (-len(token) % 4))
    obj = json.loads(raw)
    obj['name'] = display_name if display_name is not None else (entry['name'] if exit_row is None else exit_row['name'] + ' · ' + entry['name'])
    obj['transport']['url'] = entry['url']
    if exit_row is not None: obj['transport']['egress_id'] = exit_row['id']
    raw = json.dumps(obj, ensure_ascii=False, separators=(',', ':')).encode('utf-8')
    return 'mbox://' + base64.urlsafe_b64encode(raw).decode('ascii').rstrip('=') + '#' + entry['id']


def is_expired(value):
    if not value:
        return False
    try:
        x = datetime.fromisoformat(value.replace('Z', '+00:00'))
        if x.tzinfo is None:
            return True  # fail closed for malformed legacy records
        return x <= datetime.now(timezone.utc)
    except (ValueError, TypeError):
        return True


def subscription_document(token, extended=False):
    if not TOKEN.fullmatch(token):
        return None
    digest = hashlib.sha256(token.encode('ascii')).hexdigest()
    with db(open_only=True) as con:
        row = con.execute('''SELECT u.id, u.enabled, u.expires_at, u.max_sessions
           FROM subscription_tokens t JOIN users u ON u.id=t.user_id
           WHERE t.digest=? AND t.revoked_at IS NULL''', (digest,)).fetchone()
    if row is None or not row['enabled'] or is_expired(row['expires_at']):
        return None
    endpoints = configured_endpoints()
    ctl = ctl_module()
    exits = configured_exits() if extended else []
    if exits:
        # Preserve existing ingress profile IDs, so an update never silently
        # redirects a beta.4.3-selected profile to a different country.
        if len(endpoints)*(len(exits)+1)>MAX_PROFILES:
            fail('too many ingress-exit combinations (max 12)')
        base_name = default_exit_label()
        # For a single ingress display only the country/exit label, not the
        # implementation detail "Primary". Keep all endpoint IDs unchanged.
        def display(exit_name, endpoint):
            return exit_name if len(endpoints) == 1 else exit_name + ' · ' + endpoint['name']
        profiles = [{'id': x['id'], 'name': display(base_name, x),
                     'link': profile_link(ctl, row['id'], x, display_name=display(base_name, x))}
                    for x in endpoints]
        profiles += [{'id': x['id'] + '_' + e['id'], 'name': display(e['name'], x),
                      'link': profile_link(ctl, row['id'], x, e, display_name=display(e['name'], x))}
                     for x in endpoints for e in exits]
    else:
        profiles = [{'id': x['id'], 'name': x['name'], 'link': profile_link(ctl,row['id'],x)}
                    for x in endpoints]
    # Fail closed before emitting an unusable Android bundle when ingress and
    # exit IDs collide or their combination exceeds the 32-byte profile ID cap.
    ids=[p['id'] for p in profiles]
    if len(ids)!=len(set(ids)) or any(not ENDPOINT_ID.fullmatch(x) for x in ids):
        fail('duplicate or oversized subscription profile ID')
    return {
        'version': 2 if exits else 1,
        'generated_at': datetime.now(timezone.utc).isoformat(),
        'account': {'expires_at': row['expires_at'],
                    'session_limit': row['max_sessions'],
                    'device_limit': None, 'device_used': None,
                    'traffic_limit_bytes': None, 'traffic_used_bytes': None},
        'profiles': profiles,
    }


class BoundedServer(ThreadingHTTPServer):
    daemon_threads = True
    block_on_close = False
    request_queue_size = 16
    max_requests = 16
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.slots = threading.BoundedSemaphore(self.max_requests)
    def process_request(self, request, client_address):
        if not self.slots.acquire(blocking=False):
            try: request.close()
            except OSError: pass
            return
        try:
            super().process_request(request, client_address)
        except Exception:
            self.slots.release()
            raise
    def process_request_thread(self, request, client_address):
        try:
            super().process_request_thread(request, client_address)
        finally:
            self.slots.release()

class SubscriptionHandler(BaseHTTPRequestHandler):
    server_version = 'MBoxSubscriptions'
    sys_version = ''
    protocol_version = 'HTTP/1.0'

    def log_message(self, fmt, *args):
        # Never log URL tokens, profile contents or PSKs.
        pass

    def setup(self):
        super().setup()
        self.connection.settimeout(6)

    def do_GET(self):
        p = self.path
        status = 200
        payload = None
        if len(p) > 200 or '?' in p or '#' in p or not p.startswith(PREFIX):
            status = 404
        else:
            try:
                payload = subscription_document(p[len(PREFIX):], extended=('application/vnd.mbox.subscription.v2+json' in self.headers.get('Accept', '')))
            except (sqlite3.Error, OSError, ValueError, KeyError, ImportError):
                status = 503
            if payload is None and status == 200:
                status = 404
        body = (json.dumps(payload, ensure_ascii=False, separators=(',', ':')) if payload is not None else '').encode('utf-8')
        self.send_response(status)
        self.send_header('Content-Type', 'application/json; charset=utf-8')
        self.send_header('Content-Length', str(len(body)))
        self.send_header('Cache-Control', 'no-store, private, max-age=0')
        self.send_header('Pragma', 'no-cache')
        self.send_header('X-Content-Type-Options', 'nosniff')
        self.send_header('Referrer-Policy', 'no-referrer')
        self.end_headers()
        self.wfile.write(body)

    def do_HEAD(self):
        self.send_response(405)
        self.send_header('Content-Length', '0')
        self.end_headers()

    def do_POST(self):
        self.send_response(405)
        self.send_header('Content-Length', '0')
        self.end_headers()


def command(args):
    if args.operation == 'check':
        configured_endpoints()
        configured_exits()
        default_exit_label()
        public_url()
        print('CONFIG OK: endpoints='+str(len(configured_endpoints())))
        return
    if args.operation == 'exit-default-get':
        print(default_exit_label())
        return
    if args.operation == 'exit-default-set':
        write_default_exit_label(args.name)
        print('DEFAULT EXIT LABEL SAVED')
        return
    if args.operation == 'exit-list':
        for row in configured_exits():
            print(row['id'],row['name'],row['upstream'],sep='\t')
        return
    if args.operation == 'exit-set':
        rows=[row for row in configured_exits() if row['id'] != args.id]
        rows.append(valid_egress({'id':args.id,'name':args.name,'upstream':args.upstream}))
        write_exits(rows)
        print('EXIT SAVED:',args.id)
        return
    if args.operation == 'exit-delete':
        rows=[row for row in configured_exits() if row['id'] != args.id]
        write_exits(rows)
        print('EXIT DELETED:',args.id)
        return
    if args.operation == 'endpoint-list':
        for x in configured_endpoints():
            print(x['id'], x['name'], x['url'], sep='\t')
        return
    if args.operation == 'endpoint-set':
        entries = configured_endpoints()
        row = {'id': args.id, 'name': args.name, 'url': args.url}
        if not ENDPOINT_ID.fullmatch(row['id']) or not 1 <= len(row['name']) <= 48:
            fail('invalid ingress label or ID')
        checked_url(row['url'])
        entries = [x for x in entries if x['id'] != row['id']] + [row]
        write_endpoints(entries)
        print('ENDPOINT SAVED:', row['id'])
        return
    if args.operation == 'endpoint-delete':
        entries = [x for x in configured_endpoints() if x['id'] != args.id]
        write_endpoints(entries)
        print('ENDPOINT DELETED:', args.id)
        return
    if args.operation in ('issue', 'ensure'):
        # issue is kept as a compatibility alias, but is now idempotent.
        # Showing the same subscription again never creates another token.
        print(get_or_create_subscription(args.id))
        return
    if args.operation == 'revoke-all':
        print('REVOKED:', revoke_subscriptions(args.id))
        return
    if args.operation == 'serve':
        # Never allow serving on public interface even via misconfiguration.
        host = '127.0.0.1'
        with db() as con:
            schema(con)
        configured_endpoints()
        public_url()
        server = BoundedServer((host, args.port), SubscriptionHandler)
        server.daemon_threads = True
        print('Subscription HTTP listening on 127.0.0.1:'+str(args.port), flush=True)
        try:
            server.serve_forever(poll_interval=0.5)
        finally:
            server.server_close()
        return
    fail('unknown operation')


def main():
    p = argparse.ArgumentParser(description='MBox subscription issuer and HTTPS reverse-proxy upstream')
    sub = p.add_subparsers(dest='operation', required=True)
    sub.add_parser('check')
    sub.add_parser('exit-list')
    sub.add_parser('exit-default-get')
    a = sub.add_parser('exit-default-set'); a.add_argument('name')
    a = sub.add_parser('exit-set'); a.add_argument('id'); a.add_argument('name'); a.add_argument('upstream')
    a = sub.add_parser('exit-delete'); a.add_argument('id')
    sub.add_parser('endpoint-list')
    a = sub.add_parser('endpoint-set'); a.add_argument('id'); a.add_argument('name'); a.add_argument('url')
    a = sub.add_parser('endpoint-delete'); a.add_argument('id')
    a = sub.add_parser('issue'); a.add_argument('id')  # compatibility alias: idempotent
    a = sub.add_parser('ensure'); a.add_argument('id')
    a = sub.add_parser('revoke-all'); a.add_argument('id')
    a = sub.add_parser('serve'); a.add_argument('--port', type=int, default=18450)
    a = p.parse_args()
    try:
        command(a)
    except (ValueError, sqlite3.Error, OSError) as e:
        print('ERROR:', str(e), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
