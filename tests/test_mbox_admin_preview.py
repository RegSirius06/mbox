"""MBox Admin preview tests. These never invoke a real server/mboxctl."""
import http.client
import importlib.util
import json
from pathlib import Path
import socket
import sqlite3
import subprocess
import sys
import tempfile
import threading
import unittest

HERE = Path(__file__).resolve().parents[1]
SCRIPT = HERE / 'tools/mbox-admin/mbox_admin.py'
spec = importlib.util.spec_from_file_location('mbox_admin_preview', SCRIPT)
app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(app)


class UnixConnection(http.client.HTTPConnection):
    def __init__(self, path):
        self.unix_path = str(path)
        super().__init__('127.0.0.1', port=8765, timeout=5)

    def connect(self):
        self.sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.sock.settimeout(self.timeout)
        self.sock.connect(self.unix_path)


class AdminTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        p = Path(self.tmp.name)
        self.db, self.sock, self.pw, self.ctl, self.log = [p/name for name in
            ('users.db', 'admin.sock', 'password.json', 'mboxctl-fake', 'calls.txt')]
        con = sqlite3.connect(self.db)
        con.execute('CREATE TABLE users (id TEXT, label TEXT, enabled INTEGER, max_sessions INTEGER, '
                    'expires_at TEXT, created_at TEXT, psk BLOB)')
        con.execute('INSERT INTO users VALUES (?,?,?,?,?,?,?)',
                    ('client1', 'Private <user>', 1, 2, None, '2026-11-15', b'SECRETPSK'))
        con.commit()
        con.close()
        app.create_password_file(self.pw, 'very_secure_admin_password')
        self.ctl.write_text('#!/usr/bin/env python3\n'
                            'import sys, pathlib\n'
                            f'with pathlib.Path({str(self.log)!r}).open("a") as f: f.write(repr(sys.argv[1:])+"\\n")\n'
                            'print("mbox://CONFIDENTIAL_LINK" if sys.argv[1:3]==["user","link"] else "OK")\n')
        self.ctl.chmod(0o700)
        app.ADMIN = app.AdminConfig(self.ctl, self.db, p/'missing-status.json', self.pw)
        app.SESSIONS.clear()
        self.server = app.UnixServer(str(self.sock), app.Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)
        self.cookie = ''
        self.csrf = ''

    def req(self, method, path, data=None, headers=None):
        c = UnixConnection(self.sock)
        h = {'Host': '127.0.0.1:8765'}
        if data is not None:
            h['Content-Type'] = 'application/json'
        if self.cookie:
            h['Cookie'] = self.cookie
        if self.csrf:
            h['X-MBox-CSRF'] = self.csrf
        h.update(headers or {})
        body = json.dumps(data).encode() if data is not None else None
        c.request(method, path, body=body, headers=h)
        r = c.getresponse()
        content = r.read()
        headers = dict(r.getheaders())
        c.close()
        try:
            return r.status, json.loads(content), headers
        except ValueError:
            return r.status, content, headers

    def login(self):
        code, data, headers = self.req('POST','/api/login',{'password':'very_secure_admin_password'})
        self.assertEqual(code,200)
        self.cookie = headers['Set-Cookie'].split(';',1)[0]
        code, data, _ = self.req('GET','/api/session')
        self.assertEqual(code,200)
        self.csrf = data['csrf']

    def test_auth_required_and_bounded_login(self):
        code, _, _ = self.req('GET','/api/users')
        self.assertEqual(code,401)
        code, _, _ = self.req('POST','/api/login',{'password':'incorrect'})
        self.assertEqual(code,401)
        self.login()
        code, data, _ = self.req('GET','/api/users')
        self.assertEqual(code,200)
        self.assertEqual(data['users'][0]['label'],'Private <user>')
        self.assertNotIn('psk',data['users'][0])
        self.assertIsNone(data['users'][0]['traffic_bytes'])

    def test_mutation_requires_csrf(self):
        self.login()
        csrf = self.csrf
        self.csrf = ''
        code, _, _ = self.req('POST','/api/action',{'action':'disable','id':'client1'})
        self.assertEqual(code,403)
        self.assertFalse(self.log.exists())
        self.csrf = csrf
        code, _, _ = self.req('POST','/api/action',{'action':'disable','id':'client1'})
        self.assertEqual(code,200)
        self.assertIn("'disable'",self.log.read_text())

    def test_cross_origin_and_host_rejected(self):
        self.login()
        code, _, _ = self.req('POST','/api/action',{'action':'disable','id':'client1'},
                              {'Origin':'https://other.example'})
        self.assertEqual(code,403)
        code, _, _ = self.req('GET','/api/users',headers={'Host':'evil.example'})
        self.assertEqual(code,403)

    def test_links_explicitly_requested_and_no_debug_flags(self):
        self.login()
        code, data, _ = self.req('POST','/api/action',{'action':'link','id':'client1'})
        self.assertEqual(code,200)
        self.assertEqual(data['link'], 'mbox://CONFIDENTIAL_LINK')
        self.assertIn("'link'",self.log.read_text())
        code, data, _ = self.req('GET','/api/users')
        self.assertNotIn('CONFIDENTIAL_LINK',json.dumps(data))
        self.assertNotIn('SECRETPSK',json.dumps(data))

    def test_reject_injection_and_invalid_limits(self):
        self.login()
        for payload in ({'action':'add','id':'--help'},
                        {'action':'limit','id':'client1','max_sessions':True},
                        {'action':'renew','id':'client1','days':0},
                        {'action':'unknown','id':'client1'}):
            code, _, _ = self.req('POST','/api/action',payload)
            self.assertEqual(code,400)
        self.assertFalse(self.log.exists())

    def test_pages_have_no_external_dependencies(self):
        code, page, headers = self.req('GET','/')
        self.assertEqual(code,200)
        self.assertIn(b'<h1>MBox ',page)
        self.assertIn('Content-Security-Policy',headers)
        self.assertNotIn(b'https://cdn.',page)

    def test_password_file_no_overwrite(self):
        with self.assertRaises(ValueError):
            app.create_password_file(self.pw,'another_long_password')
        self.assertEqual(self.pw.stat().st_mode & 0o777,0o600)


if __name__ == '__main__':
    unittest.main()
