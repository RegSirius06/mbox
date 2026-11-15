"""Regression tests for MBox QUIC DNS identity and centralized provisioning."""
import argparse
import base64
import configparser
import importlib.util
import json
from pathlib import Path
import sys
from types import SimpleNamespace

import pytest

CTL=Path(__file__).resolve().parents[1]/'tools/mboxctl/mboxctl.py'
SPEC=importlib.util.spec_from_file_location('mboxctl_domain_test',CTL)
mboxctl=importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(mboxctl)

def cp(enabled=True,server_name='node.example.test',ca_file=''):
    x=configparser.ConfigParser(interpolation=None)
    x['server']={'public_url':'https://front.example.test/edge/test','server_public_key':'/fake/pub'}
    x['quic']={'enabled':str(enabled).lower(),'url':'https://127.0.0.1:7444','server_name':server_name,'ca_file':ca_file}
    return x

def profile(monkeypatch,config):
    monkeypatch.setattr(mboxctl,'load_conf',lambda:config)
    monkeypatch.setattr(mboxctl,'get_user',lambda uid:{'auth_handle':bytes(range(16)),'psk':b'a'*32,'label':uid})
    monkeypatch.setattr(mboxctl,'read_hex32',lambda path:bytes(range(32)))
    monkeypatch.setattr(mboxctl,'camo_dict',lambda *args,**kwargs:{'open_path':'/api/v1/session'})
    link=mboxctl.build_link('alice')
    enc=link.removeprefix('mbox://').split('#')[0]
    return json.loads(base64.urlsafe_b64decode(enc+'='*((-len(enc))%4)))

def test_domain_tls_auto_populates_links_without_pem(monkeypatch):
    obj=profile(monkeypatch,cp())
    assert obj['transport']['quic_url']=='https://127.0.0.1:7444'
    assert obj['transport']['quic_server_name']=='node.example.test'
    assert 'quic_ca_pem' not in obj['transport']
    assert obj['crypto']['psk']==(b'a'*32).hex()

def test_https_only_by_default(monkeypatch):
    obj=profile(monkeypatch,cp(enabled=False))
    assert 'quic_url' not in obj['transport']

def test_legacy_explicit_link_flags_remain_supported(monkeypatch,tmp_path):
    cert=tmp_path/'ca.pem'
    # cert fixture is syntactically PEM; validation is via ssl.PEM_cert_to_DER_cert.
    cert.write_text('-----BEGIN CERTIFICATE-----\nAQIDBA==\n-----END CERTIFICATE-----\n')
    data=mboxctl.quic_profile_metadata('https://127.0.0.1:7443',str(cert),None)
    assert 'quic_ca_pem' in data
    assert 'quic_server_name' not in data

@pytest.mark.parametrize('domain', ['1.2.3.4','*.example.com','www.example.com:443','foo..bar','-foo.example','foo-.example','http://example.com'])
def test_reject_malformed_tls_names(domain):
    with pytest.raises(SystemExit):
        mboxctl.quic_profile_metadata('https://127.0.0.1:7444',None,domain)

def test_reject_missing_identity():
    with pytest.raises(SystemExit):
        mboxctl.quic_profile_metadata('https://127.0.0.1:7444',None,None)

def test_quic_config_edit_must_not_restart_noise_server(monkeypatch,capsys):
    config=cp(enabled=True)
    monkeypatch.setattr(mboxctl,'require_root',lambda:None)
    monkeypatch.setattr(mboxctl,'load_conf',lambda:config)
    monkeypatch.setattr(mboxctl,'validate_config',lambda cp:None)
    monkeypatch.setattr(mboxctl,'save_conf',lambda cp:None)
    def fail(*args,**kwargs): raise AssertionError('unexpected server restart or relay rebuild')
    monkeypatch.setattr(mboxctl,'render_runtime',fail)
    monkeypatch.setattr(mboxctl,'generate_relay_files',fail)
    monkeypatch.setattr(mboxctl,'generate_origin_files',fail)
    mboxctl.config_set(SimpleNamespace(key='quic.server_name',value='new.example.com'))
    assert config['quic']['server_name']=='new.example.com'
