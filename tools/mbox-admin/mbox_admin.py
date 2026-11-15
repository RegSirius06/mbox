#!/usr/bin/env python3
"""SSH-only MBox administration preview. Stdlib only; no public HTTP listener.

Reads the existing SQLite users table, delegates every mutation to mboxctl.
Never stores or logs provisioning links. Run behind a root-owned Unix socket.
"""
import argparse
import base64
import configparser
from collections import deque
from http.cookies import SimpleCookie
from http.server import BaseHTTPRequestHandler
import getpass
import hashlib
import hmac
import json
import os
from pathlib import Path
from importlib.util import spec_from_file_location, module_from_spec
_cfg_spec = spec_from_file_location('mbox_browser_config_settings', Path(__file__).with_name('config_ui.py'))
config_ui = module_from_spec(_cfg_spec)
_cfg_spec.loader.exec_module(config_ui)
import re
import secrets
import socket
import socketserver
import sqlite3
import stat
import subprocess
import sys
import time
from urllib.parse import urlsplit

MAX_BODY = 16 * 1024
USER_ID = re.compile(r"[A-Za-z0-9._-]{1,63}\Z")
SESSIONS = {}
SESSION_LIFETIME = 60 * 60
ADMIN = None


def validate_uid(value):
    if not isinstance(value, str) or not USER_ID.fullmatch(value) or value.startswith('-'):
        raise ValueError('Invalid user ID (1..63 characters: letters, digits, . _ -; cannot start with -)')
    return value


def password_digest(password, salt):
    if len(password) < 12:
        raise ValueError('Administrator password must be at least 12 characters')
    return hashlib.scrypt(password.encode('utf-8'), salt=salt, n=2**14, r=8, p=1, dklen=32)


def create_password_file(path, password):
    path = Path(path)
    if path.exists() or path.is_symlink():
        raise ValueError('Password file already exists; refusing to overwrite')
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    salt = secrets.token_bytes(16)
    payload = {'salt': salt.hex(), 'digest': password_digest(password, salt).hex()}
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as f:
        json.dump(payload, f)
        f.write('\n')
    os.chmod(path, 0o600)


def load_password(path):
    path = Path(path)
    if path.is_symlink() or path.stat().st_mode & 0o077:
        raise ValueError('Password file permissions must be 0600 and file must not be symlink')
    d = json.loads(path.read_text(encoding='utf-8'))
    return bytes.fromhex(d['salt']), bytes.fromhex(d['digest'])


class AdminConfig:
    def __init__(self, ctl, db, status, password_path, quic_url='', quic_ca_file=''):
        self.ctl, self.db, self.status = str(ctl), str(db), str(status)
        self.salt, self.digest = load_password(password_path)
        if bool(quic_url) != bool(quic_ca_file):
            raise ValueError('QUIC URL and CA file must be supplied together')
        self.quic_url, self.quic_ca_file = quic_url, quic_ca_file
        self.failures = deque(maxlen=10)
        self.subscription_cli = '/usr/local/bin/mbox-subscription'

    def quic_enabled(self):
        # Read only the authoritative [quic] section; no subprocess or mutations.
        if self.quic_url:
            return True  # legacy URL+CA override
        cp = configparser.ConfigParser(interpolation=None)
        try:
            if not cp.read('/etc/mbox/mbox.conf'):
                return False
            return cp.getboolean('quic', 'enabled', fallback=False)
        except (OSError, ValueError, configparser.Error):
            return False

    def authenticate(self, password):
        now = time.monotonic()
        if len(self.failures) >= 5 and now - self.failures[0] < 60:
            return False
        if not isinstance(password, str) or len(password) > 256:
            self.failures.append(now)
            return False
        try:
            derived = password_digest(password, self.salt)
        except ValueError:
            self.failures.append(now)
            return False
        success = hmac.compare_digest(derived, self.digest)
        if success:
            self.failures.clear()
        else:
            self.failures.append(now)
        return success

    def run(self, *args):
        if not os.path.isabs(self.ctl):
            raise RuntimeError('mboxctl path must be absolute')
        proc = subprocess.run([self.ctl, *args], text=True, capture_output=True,
                              timeout=15, check=False, stdin=subprocess.DEVNULL)
        if proc.returncode:
            # Do not leak secret-bearing stdout/stderr to the browser.
            raise RuntimeError(f'mboxctl exited with code {proc.returncode}; inspect local server logs')
        return proc.stdout.strip()

    def sub_run(self, *args):
        # Separate allowlisted CLI. Never invoke a shell or pass unbounded data.
        proc = subprocess.run([self.subscription_cli, *args], text=True, capture_output=True,
                              timeout=15, check=False, stdin=subprocess.DEVNULL)
        if proc.returncode:
            raise RuntimeError('subscription operation failed; inspect local server logs')
        return proc.stdout.strip()

    def users(self):
        dbpath = Path(self.db)
        connection = sqlite3.connect(dbpath.as_uri() + '?mode=ro', uri=True, timeout=3)
        connection.row_factory = sqlite3.Row
        try:
            rows = connection.execute('SELECT id, label, enabled, max_sessions, expires_at, created_at '
                                      'FROM users ORDER BY id LIMIT 5001').fetchall()
            # Safe aggregate only: never expose token digests or plaintext URLs.
            try:
                counts = {uid: n for uid, n in connection.execute(
                    'SELECT user_id, COUNT(*) FROM subscription_tokens WHERE revoked_at IS NULL GROUP BY user_id')}
            except sqlite3.OperationalError as exc:
                if 'no such table' not in str(exc):
                    raise
                counts = {}
        finally:
            connection.close()
        if len(rows) > 5000:
            raise RuntimeError('User list exceeds UI safety limit')
        try:
            snap = json.loads(Path(self.status).read_text(encoding='utf-8'))
            active = snap.get('clients') or {}
        except (OSError, ValueError, TypeError):
            active = {}
        result = []
        now = time.time()
        from datetime import datetime, timezone
        for row in rows:
            r = dict(row)
            key = 'client#' + hashlib.sha256(r['id'].encode()).digest()[:6].hex()
            r['online'] = int(active.get(key, 0)) if isinstance(active, dict) else 0
            # The DB does not yet contain per-user traffic counters; do not invent them.
            r['traffic_bytes'] = None
            r['subscription_count'] = int(counts.get(r['id'], 0))
            r['enabled'] = bool(r['enabled'])
            expiry = r['expires_at']
            if expiry:
                try:
                    r['expired'] = datetime.fromisoformat(expiry.replace('Z', '+00:00')) <= datetime.now(timezone.utc)
                except (ValueError, TypeError):
                    r['expired'] = True
            else:
                r['expired'] = False
            result.append(r)
        return result

    def execute(self, action, data):
        if action == 'config_apply':
            return config_ui.apply(data.get('revision'), data.get('changes'), data.get('confirm'))
        if action == 'qr_encode':
            link = data.get('link')
            if not isinstance(link, str) or len(link) > 32000 or '\n' in link or '\r' in link or not (link.startswith('mbox://') or (link.startswith(config_ui.snapshot()['subscription_url'] + '/') and re.fullmatch(r'[A-Za-z0-9_-]{42,48}', link.rsplit('/',1)[-1]) is not None)):
                raise ValueError('Only MBox profile or subscription links may be encoded')
            try:
                result = subprocess.run(['qrencode', '-8', '-t', 'PNG', '-o', '-', '-s', '5'],
                                        input=link.encode('utf-8'), stdout=subprocess.PIPE,
                                        stderr=subprocess.DEVNULL, timeout=5, check=False)
            except FileNotFoundError:
                raise ValueError('qrencode not installed on VPS')
            if result.returncode or not result.stdout.startswith(b'\x89PNG\r\n\x1a\n') or len(result.stdout) > 120000:
                raise ValueError('QR cannot hold this link; use subscription URL instead')
            return base64.b64encode(result.stdout).decode('ascii')
        if action == 'endpoint_list':
            return self.sub_run('endpoint-list')
        if action == 'endpoint_set':
            eid = data.get('endpoint_id', '')
            label = data.get('endpoint_name', '')
            url = data.get('endpoint_url', '')
            if not isinstance(eid, str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,32}', eid):
                raise ValueError('Invalid endpoint ID')
            if not isinstance(label, str) or not 1 <= len(label) <= 48 or '\n' in label:
                raise ValueError('Invalid endpoint label')
            if not isinstance(url, str) or len(url) > 512 or not url.startswith('https://'):
                raise ValueError('Endpoint must use HTTPS')
            return self.sub_run('endpoint-set', eid, label, url)
        if action == 'endpoint_delete':
            eid = data.get('endpoint_id', '')
            if not isinstance(eid, str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,32}', eid):
                raise ValueError('Invalid endpoint ID')
            if data.get('confirm_id') != eid:
                raise ValueError('Endpoint removal requires ID confirmation')
            return self.sub_run('endpoint-delete', eid)
        if action == 'delete_batch':
            ids = data.get('ids')
            if not isinstance(ids, list) or not 2 <= len(ids) <= 100 or len(set(map(str, ids))) != len(ids):
                raise ValueError('Select 2..100 distinct user IDs')
            for uid in ids:
                validate_uid(uid)
            if data.get('confirm') != 'DELETE ' + str(len(ids)):
                raise ValueError('Batch deletion confirmation mismatch')
            return self.run('user', 'delete', *ids, '--yes')
        if action == 'ctl_read':
            # Safe, fixed read-only commands. A root CLI shell in the browser
            # would bypass all input and confirmation rules.
            cmd = data.get('command')
            safe = {'version': ('version',), 'doctor': ('doctor',),
                    'user-list': ('user', 'list'), 'user-count': ('user', 'count'),
                    'resources': ('resources',), 'db-check': ('db', 'check')}
            if cmd not in safe:
                raise ValueError('Read-only command is not allowed')
            result = self.run(*safe[cmd])
            return result[:12000]
        if action in ('sub_issue', 'sub_revoke'):
            uid = validate_uid(data.get('id'))
            if action == 'sub_issue':
                link = self.sub_run('ensure', uid)
                if not link.startswith('https://') or '\n' in link or len(link) > 2048:
                    raise RuntimeError('invalid subscription URL')
                return link
            if data.get('confirm_id') != uid:
                raise ValueError('Revoking subscriptions requires user ID confirmation')
            return self.sub_run('revoke-all', uid)
        if action == 'add_batch':
            # Batch creation uses the CLI's existing single SQLite transaction.
            # Bound both count and generated IDs before invoking any root command.
            count = data.get('count')
            if isinstance(count, bool) or not isinstance(count, int) or not 2 <= count <= 100:
                raise ValueError('count must be an integer between 2 and 100')
            prefix = data.get('prefix')
            if not isinstance(prefix, str) or not prefix:
                raise ValueError('Invalid user prefix')
            validate_uid(prefix + '001')
            validate_uid(prefix + f'{count:03d}')
            cmd = ['user', 'add', '--count', str(count), '--prefix', prefix, '--width', '3']
            n = data.get('max_sessions')
            if n is not None:
                if isinstance(n, bool) or not isinstance(n, int) or not 1 <= n <= 10000:
                    raise ValueError('max_sessions must be 1..10000')
                cmd += ['--max-sessions', str(n)]
            days = data.get('days')
            if days is not None:
                if isinstance(days, bool) or not isinstance(days, int) or not 1 <= days <= 3650:
                    raise ValueError('days must be 1..3650')
                cmd += ['--days', str(days)]
            return self.run(*cmd)
        if action == 'add':
            uid = validate_uid(data.get('id'))
            cmd = ['user', 'add', uid]
            n = data.get('max_sessions')
            if n is not None:
                if not isinstance(n, int) or isinstance(n, bool) or not 1 <= n <= 10000:
                    raise ValueError('max_sessions must be 1..10000')
                cmd += ['--max-sessions', str(n)]
            days = data.get('days')
            if days is not None:
                if not isinstance(days, int) or isinstance(days, bool) or not 1 <= days <= 3650:
                    raise ValueError('days must be 1..3650')
                cmd += ['--days', str(days)]
            return self.run(*cmd)
        uid = validate_uid(data.get('id'))
        if action in ('enable', 'disable', 'revoke'):
            return self.run('user', action, uid)
        if action == 'rotate':
            return self.run('user', 'rotate', uid)
        if action == 'renew':
            days = data.get('days')
            if not isinstance(days, int) or isinstance(days, bool) or not 1 <= days <= 3650:
                raise ValueError('days must be 1..3650')
            return self.run('user', 'renew', uid, str(days))
        if action == 'limit':
            n = data.get('max_sessions')
            if n == -1:
                return self.run('user', 'limit', uid, '--unlimited')
            if not isinstance(n, int) or isinstance(n, bool) or not 1 <= n <= 10000:
                raise ValueError('max_sessions must be 1..10000 or -1')
            return self.run('user', 'limit', uid, str(n))
        if action == 'expiry_clear':
            return self.run('user', 'expiry', uid, '--clear')
        if action == 'delete':
            # Two independent confirmations: browser asks the operator to type
            # the exact ID, and the server enforces it before --yes reaches CLI.
            if data.get('confirm_id') != uid:
                raise ValueError('Deletion requires the exact user ID as confirmation')
            return self.run('user', 'delete', uid, '--yes')
        if action == 'link':
            cmd = ['user', 'link', uid]
            if self.quic_url:
                cmd += ['--quic-url', self.quic_url, '--quic-ca-file', self.quic_ca_file]
            link = self.run(*cmd)
            if not link.startswith('mbox://') or '\n' in link or len(link) > 32768:
                raise RuntimeError('Invalid provisioning output from mboxctl')
            return link
        raise ValueError('Unknown action')


class Handler(BaseHTTPRequestHandler):
    server_version = 'MBoxAdminPreview'
    sys_version = ''
    protocol_version = 'HTTP/1.0'

    def log_message(self, format, *args):
        # No URLs, bodies, cookies, provisioning links, or client identifiers in logs.
        pass

    def setup(self):
        super().setup()
        self.connection.settimeout(8)

    def headers_safe(self):
        host = self.headers.get('Host', '')
        return bool(re.fullmatch(r'(?:127\.0\.0\.1|localhost|\[::1\])(?::[0-9]{1,5})?', host))

    def respond(self, status, payload, cookie=None):
        data = json.dumps(payload, ensure_ascii=False, separators=(',', ':')).encode('utf-8')
        self.send_response(status)
        self.send_header('Content-Type', 'application/json; charset=utf-8')
        self.send_header('Content-Length', str(len(data)))
        self.send_header('Cache-Control', 'no-store')
        self.send_header('X-Content-Type-Options', 'nosniff')
        self.send_header('Referrer-Policy', 'no-referrer')
        self.send_header('Cross-Origin-Resource-Policy', 'same-origin')
        if cookie:
            self.send_header('Set-Cookie', cookie)
        self.end_headers()
        self.wfile.write(data)

    def session(self):
        try:
            c = SimpleCookie()
            c.load(self.headers.get('Cookie', ''))
            sid = c.get('mbox_admin')
            if sid is None:
                return None, None
            token = sid.value
            item = SESSIONS.get(token)
            if item and item['until'] > time.monotonic():
                return token, item
            SESSIONS.pop(token, None)
        except (ValueError, KeyError):
            pass
        return None, None

    def do_GET(self):
        if not self.headers_safe():
            return self.respond(403, {'error': 'Forbidden host'})
        path = urlsplit(self.path).path
        if path == '/':
            html = (Path(__file__).parent / 'index.html').read_bytes()
            self.send_response(200)
            self.send_header('Content-Type', 'text/html; charset=utf-8')
            self.send_header('Content-Length', str(len(html)))
            self.send_header('Cache-Control', 'no-store')
            self.send_header('Content-Security-Policy', "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
            self.send_header('X-Frame-Options', 'DENY')
            self.send_header('Referrer-Policy', 'no-referrer')
            self.end_headers()
            return self.wfile.write(html)
        _, session = self.session()
        if not session:
            return self.respond(401, {'error': 'Authentication required'})
        if path == '/api/session':
            return self.respond(200, {'csrf': session['csrf'], 'quic_profiles': ADMIN.quic_enabled()})
        if path == '/api/settings':
            try:return self.respond(200, config_ui.snapshot())
            except (OSError, ValueError, configparser.Error):return self.respond(503, {'error':'Cannot load configuration'})
        if path == '/api/users':
            try:
                return self.respond(200, {'users': ADMIN.users()})
            except (OSError, sqlite3.Error, RuntimeError) as e:
                return self.respond(503, {'error': 'Cannot read user database/status'})
        return self.respond(404, {'error': 'Not found'})

    def do_POST(self):
        if not self.headers_safe():
            return self.respond(403, {'error': 'Forbidden host'})
        origin = self.headers.get('Origin', '')
        if origin and origin != 'http://' + self.headers.get('Host', ''):
            return self.respond(403, {'error': 'Invalid Origin'})
        if not self.headers.get('Content-Type', '').split(';')[0].strip() == 'application/json':
            return self.respond(415, {'error': 'JSON required'})
        try:
            n = int(self.headers.get('Content-Length', '-1'))
            if n < 0 or n > MAX_BODY:
                return self.respond(413, {'error': 'Invalid body size'})
            data = json.loads(self.rfile.read(n))
            if not isinstance(data, dict):
                raise ValueError()
        except (ValueError, json.JSONDecodeError, UnicodeDecodeError):
            return self.respond(400, {'error': 'Invalid JSON'})
        path = urlsplit(self.path).path
        if path == '/api/login':
            if not ADMIN.authenticate(data.get('password')):
                return self.respond(401, {'error': 'Wrong password or rate limit'})
            for key, item in list(SESSIONS.items()):
                if item['until'] <= time.monotonic():
                    del SESSIONS[key]
            if len(SESSIONS) >= 16:
                SESSIONS.pop(next(iter(SESSIONS)))
            token = secrets.token_urlsafe(32)
            SESSIONS[token] = {'csrf': secrets.token_urlsafe(32), 'until': time.monotonic()+SESSION_LIFETIME}
            return self.respond(200, {'ok': True}, 'mbox_admin='+token+'; HttpOnly; SameSite=Strict; Path=/')
        token, session = self.session()
        if not session:
            return self.respond(401, {'error': 'Authentication required'})
        if not hmac.compare_digest(self.headers.get('X-MBox-CSRF', ''), session['csrf']):
            return self.respond(403, {'error': 'CSRF mismatch'})
        if path == '/api/logout':
            SESSIONS.pop(token, None)
            return self.respond(200, {'ok': True}, 'mbox_admin=; Max-Age=0; HttpOnly; SameSite=Strict; Path=/')
        if path != '/api/action':
            return self.respond(404, {'error': 'Not found'})
        try:
            action = data.get('action')
            if not isinstance(action, str) or action not in {'add','add_batch','enable','disable','revoke','rotate','renew','limit','expiry_clear','delete','delete_batch','link','sub_issue','sub_revoke','endpoint_list','endpoint_set','endpoint_delete','ctl_read','config_apply','qr_encode'}:
                raise ValueError('Invalid action')
            result = ADMIN.execute(action, data)
            if action in ('link', 'sub_issue'):
                return self.respond(200, {'link': result})
            if action == 'config_apply':
                return self.respond(200, result)
            if action in ('endpoint_list', 'ctl_read'):
                return self.respond(200, {'output': result})
            if action == 'qr_encode':
                return self.respond(200, {'qr_png': result})
            return self.respond(200, {'ok': True})
        except ValueError as exc:
            return self.respond(400, {'error': str(exc)})
        except RuntimeError as exc:
            # config_ui errors are fixed safe labels (never subprocess output).
            if action == 'config_apply':
                return self.respond(503, {'error': str(exc)})
            return self.respond(503, {'error': 'Command failed; inspect server locally'})
        except (OSError, subprocess.TimeoutExpired):
            return self.respond(503, {'error': 'Command failed; inspect server locally'})


class UnixServer(socketserver.UnixStreamServer):
    allow_reuse_address = True


def serve(args):
    global ADMIN
    if os.geteuid() != 0 and not args.allow_nonroot_test:
        raise SystemExit('Run as root: mboxctl mutations require root')
    ADMIN = AdminConfig(args.ctl, args.db, args.status, args.password_file, args.quic_url, args.quic_ca_file)
    path = Path(args.socket)
    if not path.is_absolute():
        raise ValueError('Socket path must be absolute')
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    parent_stat = path.parent.stat()
    if os.geteuid() == 0 and (parent_stat.st_uid != 0 or parent_stat.st_mode & 0o077):
        raise ValueError('Admin socket directory must be root-owned mode 0700')
    if path.is_symlink():
        raise ValueError('Refusing symlink socket')
    if path.exists():
        if not stat.S_ISSOCK(path.stat().st_mode):
            raise ValueError('Refusing to replace non-socket path')
        path.unlink()  # only within the secured socket directory
    old_umask = os.umask(0o077)
    try:
        with UnixServer(str(path), Handler) as server:
            os.chmod(path, 0o600)
            try:
                server.serve_forever(poll_interval=0.2)
            finally:
                path.unlink(missing_ok=True)
    finally:
        os.umask(old_umask)


def main():
    p = argparse.ArgumentParser(description='SSH-only MBox admin preview')
    sub = p.add_subparsers(dest='command', required=True)
    init = sub.add_parser('init', help='Create admin password file (interactive)')
    init.add_argument('--password-file', default='/etc/mbox-admin/password.json')
    s = sub.add_parser('serve', help='Serve a root-owned Unix socket only')
    s.add_argument('--password-file', default='/etc/mbox-admin/password.json')
    s.add_argument('--socket', default='/run/mbox-admin/admin.sock')
    s.add_argument('--ctl', default='/usr/local/bin/mboxctl')
    s.add_argument('--db', default='/var/lib/mbox/users.db')
    s.add_argument('--status', default='/run/mbox/status.json')
    s.add_argument('--quic-url', default=os.environ.get('MBOX_ADMIN_QUIC_URL', ''))
    s.add_argument('--quic-ca-file', default=os.environ.get('MBOX_ADMIN_QUIC_CA_FILE', ''))
    s.add_argument('--allow-nonroot-test', action='store_true', help=argparse.SUPPRESS)
    args = p.parse_args()
    if args.command == 'init':
        if os.geteuid() != 0:
            raise SystemExit('Admin password installation requires root')
        x = getpass.getpass('New admin password (at least 12 characters): ')
        y = getpass.getpass('Repeat password: ')
        if not hmac.compare_digest(x, y):
            raise SystemExit('Passwords differ')
        create_password_file(args.password_file, x)
        print('Password file created; do not share it.')
    else:
        serve(args)


if __name__ == '__main__':
    main()
