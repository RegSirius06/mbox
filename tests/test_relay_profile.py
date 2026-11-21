import importlib.util
import subprocess
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('mbox_relay_profile_test', ROOT/'tools/mboxctl/mboxctl.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def test_profile_defaults_and_partial_override(tmp_path, monkeypatch):
    monkeypatch.setattr(m, 'RELAY_PROFILE', tmp_path/'profile.json')
    monkeypatch.setattr(m, 'require_root', lambda: None)
    explicit = SimpleNamespace(ssh='user@host.example', remote_dir='/data/www/site', remote_private_dir='/data/private')
    m.relay_configure(explicit)
    assert m.RELAY_PROFILE.stat().st_mode & 0o777 == 0o600
    args = SimpleNamespace(ssh=None, remote_dir=None, remote_private_dir=None, origin_config=None, relay_binary=None)
    m._relay_resolve_args(args)
    assert args.ssh == explicit.ssh and args.remote_dir == explicit.remote_dir
    assert args.origin_config == str(m.RUNTIME) and args.relay_binary == str(m.RELAY_TEMPLATE)
    with pytest.raises(SystemExit):
        m._relay_profile_paths(SimpleNamespace(ssh='user@other', remote_dir=None, remote_private_dir=None))


def test_ssh_bootstrap_preserves_binary_stdin(monkeypatch):
    real_run = subprocess.run
    def local_run(cmd, **kwargs):
        assert cmd[0]=='ssh' and 'ServerAliveInterval=10' in cmd
        return real_run(['sh','-c',cmd[-1]], **kwargs)
    monkeypatch.setattr(m.subprocess, 'run', local_run)
    output = m._relay_ssh_call('user@host', 'import sys; print(sys.argv[1]); print(sys.stdin.buffer.read().hex())', ('arg with spaces',), b'\x00\xff\n')
    assert output == 'arg with spaces\n00ff0a\n'
