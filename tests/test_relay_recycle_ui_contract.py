"""Lightweight deploy/rollback and compact home contract checks."""
import ast
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def _remote_scripts():
    module = ast.parse((ROOT / 'tools/mboxctl/mboxctl.py').read_text())
    result = {}
    for node in module.body:
        if isinstance(node, ast.Assign) and isinstance(node.value, ast.Constant):
            for name in node.targets:
                if isinstance(name, ast.Name) and name.id in ('_RELAY_SSH_DEPLOY', '_RELAY_SSH_ROLLBACK'):
                    result[name.id] = node.value.value
    return result


def test_recycle_follows_atomic_deploy_and_rollback():
    scripts = _remote_scripts()
    assert len(scripts) == 2
    for name, script in scripts.items():
        compile(script, name, 'exec')
        assert 'MBOX_RELAY_RECYCLE_STALE_WORKERS_V1' in script
        assert script.index('os.replace(') < script.index('MBOX_RELAY_RECYCLE_STALE_WORKERS_V1')
        assert 'os.geteuid()' in script
        assert 'st_ino' in script and 'st_dev' in script
        assert 'SIGTERM' in script
        assert 'os.scandir(\'/proc\')' in script


def test_home_keeps_one_connect_action_and_settings():
    src = (ROOT / 'android/app/src/main/java/io/mbox/android/MainActivity.java').read_text()
    assert 'MBOX_COMPACT_HOME_V2' in src
    assert 'connectButton.setVisibility(running ? View.GONE : View.VISIBLE)' in src
    assert 'disconnectButton.setVisibility(running ? View.VISIBLE : View.GONE)' in src
    assert 'settings.setOnClickListener(v -> showSettingsHome())' in src


def test_decoy_site_has_no_remote_runtime_dependencies():
    for name in ('index.html', '404.html'):
        src = (ROOT / 'deploy/regru/site' / name).read_text()
        assert '<html lang="en">' in src
        assert 'https://' not in src and '<script' not in src
        assert 'mbox://' not in src
