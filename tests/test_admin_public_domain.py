"""Opt-in MBox Admin HTTPS hostname security; no live sockets or network calls."""
from email.message import Message
import importlib.util
import io
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / 'tools/mbox-admin/mbox_admin.py'
spec = importlib.util.spec_from_file_location('mbox_admin_public_contract', SOURCE)
app = importlib.util.module_from_spec(spec)
spec.loader.exec_module(app)


def handler(host=None, origin=None, duplicate_host=False):
    h = object.__new__(app.Handler)
    h.headers = Message()
    if host is not None:
        h.headers['Host'] = host
    if duplicate_host:
        h.headers['Host'] = '127.0.0.1:8765'
    if origin is not None:
        h.headers['Origin'] = origin
    return h


def test_ssh_default_unchanged(monkeypatch):
    monkeypatch.delenv('MBOX_ADMIN_PUBLIC_HOST', raising=False)
    assert handler('127.0.0.1:8765').expected_origin() == 'http://127.0.0.1:8765'
    assert handler('localhost').expected_origin() == 'http://localhost'
    assert handler('[::1]:8765').expected_origin() == 'http://[::1]:8765'
    assert handler('panel.example.com').expected_origin() is None
    assert handler('127.0.0.1:8765', duplicate_host=True).expected_origin() is None


def test_explicit_public_host_only_https(monkeypatch):
    monkeypatch.setenv('MBOX_ADMIN_PUBLIC_HOST', 'panel.example.com')
    assert handler('panel.example.com').expected_origin() == 'https://panel.example.com'
    assert handler('panel.example.com:443').expected_origin() == 'https://panel.example.com'
    assert handler('127.0.0.1:8765').expected_origin() == 'http://127.0.0.1:8765'
    assert handler('other.example.com').expected_origin() is None
    h = handler('evil.example.com')
    h.headers['X-Forwarded-Host'] = 'panel.example.com'
    h.headers['X-Forwarded-Proto'] = 'https'
    assert h.expected_origin() is None


def test_invalid_public_hosts_rejected(monkeypatch):
    for value in ('https://panel.example.com', 'panel.example.com:443',
                  'PANEL.example.com', '*.example.com', '127.0.0.1',
                  'panel.local', 'a..example.com', 'one/example.com',
                  'evil.example.com\nother.example.com'):
        monkeypatch.setenv('MBOX_ADMIN_PUBLIC_HOST', value)
        try:
            app.configured_public_host()
        except ValueError:
            pass
        else:
            raise AssertionError(f'public-host misconfiguration accepted: {value!r}')


def test_public_post_rejects_wrong_or_missing_origin_before_body(monkeypatch):
    monkeypatch.setenv('MBOX_ADMIN_PUBLIC_HOST', 'panel.example.com')
    for origin in (None, 'http://panel.example.com', 'https://evil.example.com', 'null'):
        h = handler('panel.example.com', origin)
        got = []
        h.respond = lambda code, payload, cookie=None: got.append((code, payload))
        h.do_POST()  # Must reject before accessing rfile
        assert got[0][0] == 403
        assert got[0][1]['error'] == 'Invalid Origin'
    h = handler('panel.example.com', 'https://panel.example.com')
    h.headers['Origin'] = 'https://panel.example.com'  # duplicated header
    got = []
    h.respond = lambda code, payload, cookie=None: got.append((code, payload))
    h.do_POST()
    assert got[0][0] == 403


def test_public_cookie_secure_ssh_cookie_unmodified(monkeypatch):
    monkeypatch.setenv('MBOX_ADMIN_PUBLIC_HOST', 'panel.example.com')
    for host, secure in (('panel.example.com', True), ('127.0.0.1:8765', False)):
        h = handler(host)
        captured = {}
        h.wfile = io.BytesIO()
        h.send_response = lambda status: captured.setdefault('status', status)
        h.send_header = lambda key, value: captured.__setitem__(key, value)
        h.end_headers = lambda: None
        h.respond(200, {'ok': True}, 'mbox_admin=test; HttpOnly; SameSite=Strict; Path=/')
        assert ('; Secure' in captured['Set-Cookie']) == secure
        assert 'HttpOnly' in captured['Set-Cookie']
        assert captured['Cache-Control'] == 'no-store'


def test_socket_and_proxy_are_explicit_not_public_tcp():
    source = SOURCE.read_text()
    assert 'os.chmod(path, 0o660 if public else 0o600)' in source
    assert 'mode != (0o750 if public else 0o700)' in source
    assert 'public = configured_public_host()' in source
    assert "--socket', default='/run/mbox-admin/admin.sock'" in source
    dropin = (ROOT / 'deploy/systemd/mbox-admin-public.conf.example').read_text()
    proxy = (ROOT / 'deploy/caddy/mbox-admin-public.caddy.example').read_text()
    assert 'Group=caddy' in dropin
    assert 'RuntimeDirectoryMode=0750' in dropin
    assert 'MBOX_ADMIN_PUBLIC_HOST=panel.example.com' in dropin
    assert 'basic_auth' in proxy
    assert 'reverse_proxy unix//run/mbox-admin/admin.sock' in proxy
    assert 'header_up Host panel.example.com' in proxy


def test_ui_describes_both_modes_not_mandatory_ssh():
    html = (ROOT / 'tools/mbox-admin/index.html').read_text()
    assert 'SSH (рекомендуется) · HTTPS-домен (по настройке)' in html
    assert 'Только локальный SSH-доступ' not in html
    assert 'MBOX_BETA43_ADMIN_UX6' in html
