import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
from types import SimpleNamespace

import pytest

ROOT = Path(__file__).resolve().parents[1]
MODULE = ROOT / 'tools' / 'mboxctl' / 'mboxctl.py'
spec = importlib.util.spec_from_file_location('mboxctl_secure_deploy_test', MODULE)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def fixture_plan(tmp_path, monkeypatch):
    host = tmp_path / 'server'
    host.mkdir()
    web = tmp_path / 'regru' / 'www' / 'example.com'
    private = tmp_path / 'regru' / 'private'
    web.mkdir(parents=True)
    private.mkdir(mode=0o700)
    (private / 'locks').mkdir(mode=0o700)
    key = host / 'secret.key'
    key.write_text('55'*32 + '\n')
    key.chmod(0o600)
    runtime = host / 'server.json'
    runtime.write_text(json.dumps({'transport': {
        'listen': '127.0.0.1:18452',
        'base_path': '/fixed-path',
        'origin_hmac_secret_file': str(key),
    }}))
    binary = host / 'relay.fcgi'
    binary.write_bytes(b'\x7fELF' + b'new-relay-binary' * 10_000)
    cfg = {
        'server': {
            'base_path': '/fixed-path',
            'origin_url': 'https://origin.example.com',
        },
        'relay': {'max_slots': '4', 'max_body_bytes': '2097152'},
    }
    monkeypatch.setattr(m, 'load_conf', lambda: cfg)
    monkeypatch.setattr(m, 'validate_config', lambda cp: None)
    args = SimpleNamespace(remote_dir=str(web), remote_private_dir=str(private),
                           origin_config=str(runtime), relay_binary=str(binary),
                           relay_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
                           site=False, dry_run=False, ssh='example.test', ftp_host=None, skip_health_check=False)
    return args, web, private, runtime, binary


def package(files):
    out = io.BytesIO()
    with tarfile.open(fileobj=out, mode='w:gz') as tf:
        for name, content in files.items():
            item = tarfile.TarInfo(name)
            item.size = len(content)
            tf.addfile(item, io.BytesIO(content))
    return out.getvalue()


def remote_run(script, web, private, data=None, *extra):
    return subprocess.run([sys.executable, '-c', script, str(web), str(private), *extra],
                          input=data, capture_output=True, timeout=15)


def test_parser_supports_safe_deploy_and_rollback():
    p = m.make_parser()
    args = p.parse_args(['relay', 'deploy', '--ssh', 'host', '--remote-dir', '/www/site',
                         '--remote-private-dir', '/data/private',
                         '--origin-config', '/etc/mbox-stage/server.json',
                         '--relay-binary', '/opt/relay.fcgi', '--dry-run'])
    assert args.dry_run and args.remote_private_dir == '/data/private'
    args = p.parse_args(['relay', 'rollback', '--ssh', 'host', '--remote-dir', '/www/site',
                         '--remote-private-dir', '/data/private'])
    assert args.backup_id == 'latest'


def test_payload_uses_staging_key_and_explicit_binary(tmp_path, monkeypatch):
    args, web, private, _, binary = fixture_plan(tmp_path, monkeypatch)
    _, _, digest, files = m._relay_deploy_payload(args)
    config = json.loads(files['relay.json'])
    assert config['origin_hmac_secret_file'] == str(private / 'origin-hmac.key')
    assert config['runtime_dir'] == str(private / 'locks')
    assert config['allowed_prefixes'] == ['/fixed-path/api/v1/']
    assert 'slot_lock_prefix' not in config
    assert files['origin-hmac.key'] == ('55'*32 + '\n').encode()
    assert files['relay.fcgi'] == binary.read_bytes()
    assert digest == args.relay_sha256


def test_refuses_key_under_webroot_and_hash_mismatch(tmp_path, monkeypatch):
    args, web, private, _, _ = fixture_plan(tmp_path, monkeypatch)
    args.remote_private_dir = str(web / 'private')
    with pytest.raises(SystemExit):
        m._relay_deploy_payload(args)
    args.remote_private_dir = str(private)
    args.relay_sha256 = '0'*64
    with pytest.raises(SystemExit):
        m._relay_deploy_payload(args)


def test_refuses_wrong_origin_base_and_missing_key(tmp_path, monkeypatch):
    args, _, _, runtime, _ = fixture_plan(tmp_path, monkeypatch)
    cfg = json.loads(runtime.read_text())
    cfg['transport']['base_path'] = '/other'
    runtime.write_text(json.dumps(cfg))
    with pytest.raises(SystemExit):
        m._relay_deploy_payload(args)
    cfg['transport']['base_path'] = '/fixed-path'
    cfg['transport']['origin_hmac_secret_file'] = '/nonexistent/test.key'
    runtime.write_text(json.dumps(cfg))
    with pytest.raises(SystemExit):
        m._relay_deploy_payload(args)


def test_remote_deploy_then_rollback_restores_binary_config_secret(tmp_path, monkeypatch):
    args, web, private, _, _ = fixture_plan(tmp_path, monkeypatch)
    old_config = json.dumps({'target': 'https://origin.example.com',
                             'allowed_prefixes': ['/fixed-path/api/v1/'],
                             'slot_lock_prefix': '/tmp/old'})
    (web / 'relay.fcgi').write_bytes(b'OLD')
    (web / 'relay.json').write_text(old_config)
    (private / 'origin-hmac.key').write_text('aa'*32+'\n')
    (web / '.htaccess').write_text('DO NOT TOUCH')
    web.joinpath('index.html').write_text('ORIGINAL SITE')
    _, _, digest, files = m._relay_deploy_payload(args)
    r = remote_run(m._RELAY_SSH_DEPLOY, web, private, package(files))
    assert r.returncode == 0, r.stderr.decode()
    assert b'DEPLOY_OK' in r.stdout
    assert digest.encode() in r.stdout
    assert (web / 'relay.fcgi').read_bytes() == files['relay.fcgi']
    assert (web / 'relay.json').read_bytes() == files['relay.json']
    assert (private / 'origin-hmac.key').read_bytes() == files['origin-hmac.key']
    assert (private / 'origin-hmac.key').stat().st_mode & 0o777 == 0o600
    assert (web / 'relay.json').stat().st_mode & 0o777 == 0o600
    assert (web / 'relay.fcgi').stat().st_mode & 0o777 == 0o755
    assert (web / '.htaccess').read_text() == 'DO NOT TOUCH'
    assert (web / 'index.html').read_text() == 'ORIGINAL SITE'
    assert any((private / 'backups').iterdir())
    r = remote_run(m._RELAY_SSH_ROLLBACK, web, private, None, 'latest')
    assert r.returncode == 0, r.stderr.decode()
    assert b'ROLLBACK_OK' in r.stdout
    assert (web / 'relay.fcgi').read_bytes() == b'OLD'
    assert (web / 'relay.json').read_text() == old_config
    assert (private / 'origin-hmac.key').read_text() == 'aa'*32+'\n'


def test_remote_refuses_different_site_before_writing(tmp_path, monkeypatch):
    args, web, private, _, _ = fixture_plan(tmp_path, monkeypatch)
    (web / 'relay.json').write_text(json.dumps({'target': 'https://different.example.com',
                                               'allowed_prefixes': ['/bad/']}))
    old = (web / 'relay.json').read_bytes()
    _, _, _, files = m._relay_deploy_payload(args)
    r = remote_run(m._RELAY_SSH_DEPLOY, web, private, package(files))
    assert r.returncode != 0
    assert b'config mismatch' in r.stderr
    assert (web / 'relay.json').read_bytes() == old
    assert not (web / 'relay.fcgi').exists()


def test_deploy_dry_run_does_not_contact_ssh(tmp_path, monkeypatch, capsys):
    args, _, _, _, _ = fixture_plan(tmp_path, monkeypatch)
    args.dry_run = True
    monkeypatch.setattr(m, 'require_root', lambda: None)
    monkeypatch.setattr(m, '_relay_ssh_call', lambda *a: pytest.fail('SSH should not be called'))
    m.relay_deploy(args)
    assert 'DEPLOY PREFLIGHT OK' in capsys.readouterr().out


def test_postdeploy_health_failure_automatically_rolls_back(tmp_path, monkeypatch):
    args, _, _, _, _ = fixture_plan(tmp_path, monkeypatch)
    monkeypatch.setattr(m, 'require_root', lambda: None)
    seen = []
    def fake_ssh(host, script, argv, data=None):
        seen.append((script, argv))
        if script == m._RELAY_SSH_DEPLOY:
            return 'DEPLOY_OK\nBACKUP_ID=20261104-203000-1234abcd\n'
        return 'ROLLBACK_OK\n'
    monkeypatch.setattr(m, '_relay_ssh_call', fake_ssh)
    monkeypatch.setattr(m, '_relay_public_health', lambda: (_ for _ in ()).throw(RuntimeError('HTTP 404')))
    with pytest.raises(SystemExit):
        m.relay_deploy(args)
    assert len(seen) == 2
    assert seen[0][0] == m._RELAY_SSH_DEPLOY
    assert seen[1][0] == m._RELAY_SSH_ROLLBACK
    assert seen[1][1][2] == '20261104-203000-1234abcd'


def test_ssh_rollback_failure_reports_short_error(monkeypatch, capsys):
    def fail(cmd, **kwargs):
        raise subprocess.CalledProcessError(
            1, cmd, stderr=b'ROLLBACK_FAILED: no backups directory\n'
        )
    monkeypatch.setattr(m.subprocess, 'run', fail)
    with pytest.raises(SystemExit) as exc:
        m._relay_ssh_call('example.test', m._RELAY_SSH_ROLLBACK,
                          ('/example/web', '/example/private', 'latest'))
    assert exc.value.code == 1
    message = capsys.readouterr().err
    assert 'ROLLBACK_FAILED: no backups directory' in message
    assert 'python3 -c' not in message
    assert 'Traceback' not in message


def test_ssh_timeout_is_actionable(monkeypatch, capsys):
    def timeout(cmd, **kwargs):
        raise subprocess.TimeoutExpired(cmd, 180)
    monkeypatch.setattr(m.subprocess, 'run', timeout)
    with pytest.raises(SystemExit) as exc:
        m._relay_ssh_call('example.test', m._RELAY_SSH_DEPLOY,
                          ('/example/web', '/example/private'))
    assert exc.value.code == 1
    assert 'SSH timed out' in capsys.readouterr().err
