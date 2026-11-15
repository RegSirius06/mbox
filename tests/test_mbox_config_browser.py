import importlib.util
import os
from pathlib import Path
import pytest

ROOT=Path(__file__).resolve().parents[1]
PATH=ROOT/'tools/mbox-admin/config_ui.py'
spec=importlib.util.spec_from_file_location('mbox_config_ui_for_test',PATH)
config=importlib.util.module_from_spec(spec)
spec.loader.exec_module(config)

CONFIG=b"""[server]
public_url = https://reg.ru/api
listen = 127.0.0.1:18449
[quic]
enabled = true
server_name = lupka.zalupka.fun
[limits]
max_sessions_global = 128
"""


def test_config_in_place_keeps_comments_and_sections():
    raw='; important note\n'+CONFIG.decode()
    updated=config.modify_ini(raw,[('limits','max_sessions_global','256')])
    assert '; important note' in updated
    assert 'max_sessions_global = 256' in updated
    assert 'listen = 127.0.0.1:18449' in updated


def test_add_subscription_url():
    text=config.modify_ini(CONFIG.decode(),[('subscription','public_url','https://lupka.zalupka.fun/api/v2')])
    cp=config.parse(text.encode())
    assert config.original_url(cp)=='https://lupka.zalupka.fun/api/v2'
    assert config.check_public_url(config.original_url(cp),cp)=='/api/v2'


def test_route_single_canonical_path():
    old='''lupka.zalupka.fun {
    handle /mbox/sub/* {
        log_skip
        reverse_proxy 127.0.0.1:18450
    }
}
'''
    new=config.switch_route(old,'/mbox/sub','/api/v2')
    assert 'handle /api/v2/*' in new
    assert 'uri replace /api/v2/ /mbox/sub/ 1' in new
    assert 'handle /mbox/sub/*' not in new
    again=config.switch_route(new,'/api/v2','/video/edge')
    assert 'handle /video/edge/*' in again
    assert 'handle /api/v2/*' not in again
    assert again.count('uri replace')==1


def test_route_unsafe_configs_rejected():
    with pytest.raises(ValueError):config.switch_route('example.com {}','/mbox/sub','/api/v2')
    with pytest.raises(ValueError):config.modify_ini(CONFIG.decode(),[('server','new_secret','abc')])
    for path in ['/', '/api//v2', '/api/v2?x=1']:
        cp=config.parse(CONFIG)
        with pytest.raises(ValueError):config.check_public_url('https://lupka.zalupka.fun'+path,cp)


def test_snapshot_redacts_secrets(tmp_path,monkeypatch):
    conf=tmp_path/'mbox.conf'
    conf.write_text(CONFIG.decode()+'[auth]\nsecret = SUPERSECRET\n')
    monkeypatch.setattr(config,'CONFIG',conf)
    snapshot=config.snapshot()
    assert 'SUPERSECRET' not in str(snapshot)
    assert snapshot['sections']['auth'][0]['secret']
    assert snapshot['subscription_url'].endswith('/mbox/sub')


def test_android_subscription_path_dynamic():
    text=(ROOT/'android/app/src/main/java/io/mbox/android/SubscriptionStore.java').read_text()
    assert '"/mbox/sub/[A-Za-z0-9_-]{42,48}"' not in text
    assert 'uri.getRawPath().matches' in text


def test_apply_route_success_and_no_alias(tmp_path,monkeypatch):
    import hashlib
    cp_path=tmp_path/'mbox.conf';cp_path.write_bytes(CONFIG)
    caddy_path=tmp_path/'Caddyfile'
    caddy_path.write_text('lupka.zalupka.fun {\n    handle /mbox/sub/* {\n        log_skip\n        reverse_proxy 127.0.0.1:18450\n    }\n}\n')
    monkeypatch.setattr(config,'CONFIG',cp_path)
    monkeypatch.setattr(config,'CADDY',caddy_path)
    monkeypatch.setattr(config,'BACKUPS',tmp_path/'backups')
    called=[]
    monkeypatch.setattr(config,'call',lambda args:called.append(args))
    result=config.apply(hashlib.sha256(CONFIG).hexdigest(),[
        {'section':'subscription','key':'public_url','value':'https://lupka.zalupka.fun/api/v2'}], 'APPLY')
    assert result['url_changed']
    assert 'handle /api/v2/*' in caddy_path.read_text()
    assert 'handle /mbox/sub/*' not in caddy_path.read_text()
    assert 'https://lupka.zalupka.fun/api/v2' in cp_path.read_text()
    assert any(a[:2]==['systemctl','reload'] for a in called)
    assert not any(a[-1]=='--restart' for a in called)


def test_apply_failure_rolls_back_config_and_route(tmp_path,monkeypatch):
    import hashlib
    cp_path=tmp_path/'mbox.conf';cp_path.write_bytes(CONFIG)
    route='lupka.zalupka.fun {\n    handle /mbox/sub/* {\n        reverse_proxy 127.0.0.1:18450\n    }\n}\n'
    caddy_path=tmp_path/'Caddyfile';caddy_path.write_text(route)
    monkeypatch.setattr(config,'CONFIG',cp_path)
    monkeypatch.setattr(config,'CADDY',caddy_path)
    monkeypatch.setattr(config,'BACKUPS',tmp_path/'backups')
    def fail_after_update(args):
        if args[:2]==['caddy','validate']:
            raise RuntimeError('mock validate failure')
    monkeypatch.setattr(config,'call',fail_after_update)
    with pytest.raises(RuntimeError):
        config.apply(hashlib.sha256(CONFIG).hexdigest(),[
            {'section':'subscription','key':'public_url','value':'https://lupka.zalupka.fun/api/v2'}], 'APPLY')
    assert cp_path.read_bytes()==CONFIG
    assert caddy_path.read_text()==route


def test_apply_prevents_stale_revisions_and_unknown_keys(tmp_path,monkeypatch):
    cp_path=tmp_path/'mbox.conf';cp_path.write_bytes(CONFIG)
    monkeypatch.setattr(config,'CONFIG',cp_path)
    with pytest.raises(ValueError,match='changed'):
        config.apply('0'*64,[{'section':'limits','key':'max_sessions_global','value':'90'}],'APPLY')
    with pytest.raises(ValueError,match='Unknown'):
        import hashlib
        config.apply(hashlib.sha256(CONFIG).hexdigest(),[
            {'section':'limits','key':'invented','value':'90'}],'APPLY')


def test_browser_has_config_forms_not_cli_console():
    ui=(ROOT/'tools/mbox-admin/index.html').read_text()
    assert 'async function readConfig()' in ui
    assert 'function renderConfig()' in ui
    assert 'config_apply' in ui
    assert 'id="endpointform"' in ui
    assert 'id="cfgSection"' in ui
    assert 'id="cliraw"' not in ui
    assert 'id="execctl"' not in ui


def test_panel_settings_are_authenticated():
    s=(ROOT/'tools/mbox-admin/mbox_admin.py').read_text()
    assert "if path == '/api/settings':" in s
    assert s.index('_, session = self.session()') < s.index("if path == '/api/settings':")
    assert "if action == 'config_apply':" in s
    assert 'ctl_exec' not in s
