"""Fresh beta.4.2 admin/subscription install must include browser config UI."""
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]


def test_admin_service_sandbox_preserves_protection_with_editor_access():
    unit = (ROOT / 'deploy/systemd/mbox-admin.service').read_text()
    assert 'ProtectSystem=strict' in unit
    assert 'ProtectHome=true' in unit
    assert 'Environment=MBOX_CONFIG_BACKUPS=/var/lib/mbox/config-backups' in unit
    paths = next(x for x in unit.splitlines() if x.startswith('ReadWritePaths='))
    assert '/etc/caddy' in paths.split()
    assert '/var/lib/mbox' in paths.split()


def test_both_install_modes_ship_browser_config_module():
    source = (ROOT / 'install.sh').read_text()
    installed = 'install -m 0644 "$ROOT/tools/mbox-admin/config_ui.py" /usr/local/lib/mbox-admin/config_ui.py'
    assert source.count(installed) == 2
    assert source.count('install -d -m 0700 /var/lib/mbox/config-backups') == 2
    assert 'tools/mbox-admin/config_ui.py tools/mbox-admin/index.html' in source


def test_subscription_rollback_includes_new_module_and_unit():
    source=(ROOT/'install.sh').read_text()
    backup=source.split('if (( WITH_SUBSCRIPTIONS )); then',1)[1].split('if (( WITH_ADMIN )); then',1)[0]
    assert backup.count('/usr/local/lib/mbox-admin/config_ui.py /usr/local/lib/mbox-admin/index.html;') == 2
    assert backup.count('/etc/systemd/system/mbox-admin.service \\') >= 2
    assert 'install -m 0644 "$ROOT/deploy/systemd/mbox-admin.service" /etc/systemd/system/mbox-admin.service' in backup
