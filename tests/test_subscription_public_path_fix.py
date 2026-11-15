"""Subscription path regression tests, no network or production mutations."""
import hashlib
import importlib.util
from pathlib import Path
import pytest

ROOT=Path(__file__).resolve().parents[1]

def mod(rel,name):
    spec=importlib.util.spec_from_file_location(name,ROOT/rel)
    m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m);return m

sub=mod('tools/mbox-subscription/mbox_subscription.py','sub_path_test')
cfg=mod('tools/mbox-admin/config_ui.py','cfg_path_test')

@pytest.mark.parametrize('path',['/api/v2','/video/edge','/mbox/sub'])
def test_public_url_accepts_configured_safe_path(tmp_path,monkeypatch,path):
    pathfile=tmp_path/'mbox.conf'
    pathfile.write_text('[quic]\nserver_name = vpn.example\n[subscription]\npublic_url = https://vpn.example'+path+'\n')
    monkeypatch.setattr(sub,'CONF',pathfile)
    assert sub.public_url()=='https://vpn.example'+path

@pytest.mark.parametrize('url',[
    'https://other.example/api/v2','http://vpn.example/api/v2',
    'https://vpn.example/api//v2','https://vpn.example/api/v2/',
    'https://vpn.example/api/v2?x=1','https://vpn.example/api/v2#x',
    'https://vpn.example:7443/api/v2','https://user@vpn.example/api/v2'])
def test_public_url_refuses_wrong_host_and_unsafe_path(tmp_path,monkeypatch,url):
    pathfile=tmp_path/'mbox.conf';pathfile.write_text('[quic]\nserver_name = vpn.example\n[subscription]\npublic_url = '+url+'\n')
    monkeypatch.setattr(sub,'CONF',pathfile)
    with pytest.raises(ValueError):sub.public_url()

CONF=b'[server]\npublic_url = https://relay.example/mbox\n[quic]\nserver_name = vpn.example\n'
ROUTE=b'vpn.example {\n    handle /mbox/sub/* {\n        log_skip\n        reverse_proxy 127.0.0.1:18450\n    }\n}\n'

def test_apply_checks_subscription_and_rewrites_caddy(tmp_path,monkeypatch):
    conf=tmp_path/'mbox.conf';conf.write_bytes(CONF)
    caddy=tmp_path/'Caddyfile';caddy.write_bytes(ROUTE)
    monkeypatch.setattr(cfg,'CONFIG',conf);monkeypatch.setattr(cfg,'CADDY',caddy);monkeypatch.setattr(cfg,'BACKUPS',tmp_path/'backups')
    calls=[];monkeypatch.setattr(cfg,'call',lambda cmd:calls.append(cmd))
    cfg.apply(hashlib.sha256(CONF).hexdigest(),[{'section':'subscription','key':'public_url','value':'https://vpn.example/api/v2'}],'APPLY')
    assert conf.read_text().endswith('public_url = https://vpn.example/api/v2\n')
    assert 'handle /api/v2/*' in caddy.read_text()
    assert ['/usr/local/bin/mbox-subscription','check'] in calls
    assert not any(cmd[-1]=='--restart' for cmd in calls)


def test_failed_subscription_preflight_rolls_back_before_caddy(tmp_path,monkeypatch):
    conf=tmp_path/'mbox.conf';conf.write_bytes(CONF)
    caddy=tmp_path/'Caddyfile';caddy.write_bytes(ROUTE)
    monkeypatch.setattr(cfg,'CONFIG',conf);monkeypatch.setattr(cfg,'CADDY',caddy);monkeypatch.setattr(cfg,'BACKUPS',tmp_path/'backups')
    def call(cmd):
        if cmd[:2]==['/usr/local/bin/mbox-subscription','check']:
            raise RuntimeError('Cannot apply changes: subscription config validation failed; previous config restored')
    monkeypatch.setattr(cfg,'call',call)
    with pytest.raises(RuntimeError,match='subscription config validation'):
        cfg.apply(hashlib.sha256(CONF).hexdigest(),[{'section':'subscription','key':'public_url','value':'https://vpn.example/api/v2'}],'APPLY')
    assert conf.read_bytes()==CONF
    assert caddy.read_bytes()==ROUTE
