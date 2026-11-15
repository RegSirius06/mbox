"""Unit tests for optional beta.4.2 subscriptions; no production/network calls."""
import base64
import configparser
import importlib.util
import json
from pathlib import Path
import sqlite3
import tempfile
from unittest.mock import Mock
import pytest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('mbox_sub_v42', ROOT / 'tools/mbox-subscription/mbox_subscription.py')
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)


@pytest.fixture
def sandbox(tmp_path, monkeypatch):
    etc = tmp_path / 'etc'
    etc.mkdir()
    state = tmp_path / 'state'
    state.mkdir()
    cp = configparser.ConfigParser(interpolation=None)
    cp['server'] = {'public_url': 'https://reg.example/mbox'}
    cp['quic'] = {'server_name': 'vps.example'}
    with (etc / 'mbox.conf').open('w') as f:
        cp.write(f)
    con = sqlite3.connect(state / 'users.db')
    con.execute('CREATE TABLE users (id TEXT PRIMARY KEY, enabled INTEGER, expires_at TEXT, max_sessions INTEGER)')
    con.execute('INSERT INTO users VALUES(?,?,?,?)', ('alice', 1, None, 2))
    con.commit()
    con.close()
    stub = tmp_path / 'mboxctl'
    stub.write_text('''import base64, json\ndef build_link(uid):\n    v={"v":1,"name":uid,"client_id":"0123456789abcdef0123456789abcdef", "transport":{"url":"https://reg.example/mbox","quic_url":"https://1.2.3.4:7443","quic_server_name":"vps.example"}, "crypto":{"psk":"11"*32,"server_public_key":"22"*32},"camouflage":{}}\n    return "mbox://"+base64.urlsafe_b64encode(json.dumps(v).encode()).decode().rstrip("=")+"#"+uid\n''')
    monkeypatch.setattr(mod, 'DB', state / 'users.db')
    monkeypatch.setattr(mod, 'CONF', etc / 'mbox.conf')
    monkeypatch.setattr(mod, 'INGRESS', etc / 'subscription-endpoints.json')
    monkeypatch.setattr(mod, 'CTL', stub)
    return state / 'users.db'


def issue_token(db):
    with mod.db() as con:
        mod.schema(con)
        t='A' * 43
        con.execute('INSERT INTO subscription_tokens VALUES(?,?,?,NULL)',
                    (__import__('hashlib').sha256(t.encode()).hexdigest(), 'alice', 1))
    return t


def decode(link):
    raw=link[7:].split('#',1)[0]
    return json.loads(base64.urlsafe_b64decode(raw+'='*(-len(raw)%4)))


def test_sub_url_prefers_server_tls_domain(sandbox):
    assert mod.public_url() == 'https://vps.example/mbox/sub'


def test_subscription_is_bearer_token_and_reuses_mboxctl_profile(sandbox):
    token = issue_token(sandbox)
    obj = mod.subscription_document(token)
    assert obj['version'] == 1
    assert len(obj['profiles']) == 1
    raw = decode(obj['profiles'][0]['link'])
    assert raw['transport']['url'] == 'https://reg.example/mbox'
    assert raw['transport']['quic_server_name'] == 'vps.example'
    assert raw['crypto']['psk'] == '11'*32
    assert obj['account']['device_limit'] is None   # do not invent implemented quotas
    assert obj['account']['traffic_used_bytes'] is None
    assert mod.subscription_document('B'*43) is None


def test_multiple_ingress_preserves_credential_and_noise(sandbox):
    mod.write_endpoints([
        {'id':'reg','name':'REG.RU','url':'https://reg.example/mbox'},
        {'id':'beget','name':'Beget','url':'https://beget.example/vpn'}])
    token=issue_token(sandbox)
    profiles=mod.subscription_document(token)['profiles']
    a,b=map(lambda x:decode(x['link']), profiles)
    assert [a['transport']['url'],b['transport']['url']] == ['https://reg.example/mbox','https://beget.example/vpn']
    assert a['crypto']==b['crypto']
    assert a['camouflage']==b['camouflage']
    assert a['transport']['quic_url']==b['transport']['quic_url']


def test_revoked_and_expired_refuse_to_expose_psk(sandbox):
    token=issue_token(sandbox)
    with mod.db() as con:
        con.execute('UPDATE users SET expires_at=? WHERE id=?',('2026-01-01T00:00:00+00:00','alice'))
    assert mod.subscription_document(token) is None
    with mod.db() as con:
        con.execute('UPDATE users SET expires_at=NULL')
        con.execute('UPDATE subscription_tokens SET revoked_at=1')
    assert mod.subscription_document(token) is None


def test_disabled_user_refuses_to_expose_psk(sandbox):
    token=issue_token(sandbox)
    with mod.db() as con: con.execute('UPDATE users SET enabled=0')
    assert mod.subscription_document(token) is None


@pytest.mark.parametrize('url', ['http://host/mbox','https://host/mbox/','https://user:secret@host/mbox',
                                  'https://host/mbox?secret=1','https://host/mbox#x', 'https://host:99999/mbox'])
def test_invalid_ingress_refused(sandbox,url):
    with pytest.raises(ValueError):mod.checked_url(url)


def test_endpoint_limit_and_duplicates(sandbox):
    with pytest.raises(ValueError):mod.write_endpoints([])
    with pytest.raises(ValueError):mod.write_endpoints([{'id':'x','name':'X','url':'http://example'}])
    mod.write_endpoints([{'id':'a','name':'Name','url':'https://a.example/mbox'}])
    assert mod.configured_endpoints()[0]['id']=='a'


def test_panel_batch_delete_requires_exact_phrase(sandbox):
    path=ROOT/'tools/mbox-admin/mbox_admin.py'
    spec=importlib.util.spec_from_file_location('mbox_admin_sub_test',path)
    m=importlib.util.module_from_spec(spec)
    spec.loader.exec_module(m)
    obj=object.__new__(m.AdminConfig)
    obj.run=Mock(return_value='Deleted 2 users')
    with pytest.raises(ValueError):obj.execute('delete_batch',{'ids':['alice','bob'],'confirm':'DELETE 1'})
    obj.run.assert_not_called()
    assert obj.execute('delete_batch',{'ids':['alice','bob'],'confirm':'DELETE 2'})=='Deleted 2 users'
    obj.run.assert_called_once_with('user','delete','alice','bob','--yes')


def test_panel_subscription_only_from_fixed_cli(sandbox):
    path=ROOT/'tools/mbox-admin/mbox_admin.py'
    spec=importlib.util.spec_from_file_location('mbox_admin_sub_test2',path)
    m=importlib.util.module_from_spec(spec)
    spec.loader.exec_module(m)
    obj=object.__new__(m.AdminConfig)
    obj.run=Mock()
    obj.sub_run=Mock(return_value='https://vps.example/mbox/sub/'+'A'*43)
    assert obj.execute('sub_issue',{'id':'alice'}).startswith('https://')
    with pytest.raises(ValueError):obj.execute('sub_revoke',{'id':'alice','confirm_id':'other'})
    with pytest.raises(ValueError):obj.execute('ctl_read',{'command':'init --force'})
    obj.run.assert_not_called()


def test_web_panel_has_no_raw_cli_action(sandbox):
    path=ROOT/'tools/mbox-admin/mbox_admin.py'
    spec=importlib.util.spec_from_file_location('mbox_admin_exec_test',path)
    m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
    admin=object.__new__(m.AdminConfig)
    with pytest.raises(ValueError):
        admin.execute('ctl_exec', {'command':'mboxctl user list','confirm':'EXECUTE'})
    assert 'ctl_exec' not in path.read_text()
    assert 'execute_cli' not in path.read_text()
    assert 'cliraw' not in (ROOT/'tools/mbox-admin/index.html').read_text()


def test_caddyfile_route_narrow_and_log_skipped():
    import importlib.util
    path=ROOT/'tools/mbox-subscription/caddy_route.py'
    spec=importlib.util.spec_from_file_location('mbox_caddy_route',path)
    m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
    x='lupka.zalupka.fun {\n    reverse_proxy 127.0.0.1:2080\n}\n'
    y=m.edit_caddy(x,'lupka.zalupka.fun')
    assert 'log_skip' in y and 'handle /mbox/sub/*' in y
    assert 'reverse_proxy 127.0.0.1:2080' in y
    with pytest.raises(ValueError):m.edit_caddy(y,'lupka.zalupka.fun')
    with pytest.raises(ValueError):m.edit_caddy('another.example {\n file_server\n}\n','lupka.zalupka.fun')


def test_subscription_server_bounded_and_token_safe(sandbox):
    assert mod.BoundedServer.max_requests <= 16
    assert mod.TOKEN.fullmatch('A'*43)
    assert not mod.TOKEN.fullmatch('A'*43 + '?tracking=1')
    with mod.db() as con: mod.schema(con)
    assert mod.subscription_document('A'*43) is None
