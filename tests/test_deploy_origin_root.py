"""Read-only deploy preflight must not mutate a running RU VPS."""
from pathlib import Path
import os
import subprocess

ROOT = Path(__file__).resolve().parents[1]
DEPLOY = ROOT / 'tools/deploy-origin-root.sh'


def test_deploy_dry_run_only_checks_origin(tmp_path):
    bindir = tmp_path / 'bin'
    bindir.mkdir()
    events = tmp_path / 'events.txt'
    ssh = bindir / 'ssh'
    ssh.write_text('#!/bin/sh\necho "ssh $*" >> "$EVENTS"\necho "RU VPS: existing root-managed origin is active"\n')
    ssh.chmod(0o755)
    scp = bindir / 'scp'
    scp.write_text('#!/bin/sh\necho "scp $*" >> "$EVENTS"\nexit 99\n')
    scp.chmod(0o755)
    env = dict(os.environ, EVENTS=str(events), PATH=str(bindir) + ':' + os.environ['PATH'])
    r = subprocess.run(['bash', str(DEPLOY), '--host', 'root@vps.example'],
                       env=env, text=True, capture_output=True)
    assert r.returncode == 0, r.stderr
    assert 'DRY RUN' in r.stdout
    assert events.read_text().count('ssh ') == 1
    assert 'scp ' not in events.read_text()


def test_deploy_rejects_non_root_ssh_target(tmp_path):
    r = subprocess.run(['bash', str(DEPLOY), '--host', 'user@vps.example'],
                       text=True, capture_output=True)
    assert r.returncode == 2
    assert 'Only root@HOST' in r.stderr
