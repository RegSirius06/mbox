"""Fail-closed installer behavior and isolated admin QUIC metadata (source-only tests)."""
from pathlib import Path
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[1]
INSTALL = ROOT / 'install.sh'
ADMIN_UNIT = ROOT / 'deploy/systemd/mbox-admin.service'
ADMIN_PY = ROOT / 'tools/mbox-admin/mbox_admin.py'


class InstallerContract(unittest.TestCase):
    def test_shell_syntax(self):
        subprocess.run(['bash', '-n', str(INSTALL)], check=True)

    def test_no_args_is_safe_even_without_root(self):
        p = subprocess.run(['bash', str(INSTALL)], capture_output=True, text=True, check=True)
        self.assertIn('NO OPERATION: production unchanged', p.stdout)

    def test_explicit_flags_and_refused_ambiguous_actions(self):
        s = INSTALL.read_text()
        self.assertIn('--upgrade-origin', s)
        self.assertIn('--with-admin', s)
        self.assertIn('--fresh-install', s)
        self.assertIn('NO OPERATION', s)
        p = subprocess.run(['bash', str(INSTALL), '--with-quic'], capture_output=True, text=True)
        self.assertNotEqual(p.returncode, 0)
        self.assertIn('REFUSED', p.stderr)
        p = subprocess.run(['bash', str(INSTALL), '--with-admin', '--upgrade-origin'], capture_output=True, text=True)
        self.assertNotEqual(p.returncode, 0)
        self.assertIn('exactly one mode', p.stderr)

    def test_origin_upgrade_is_explicit_and_backed_up(self):
        s = INSTALL.read_text()
        self.assertIn('go build', s)
        self.assertIn('GOTOOLCHAIN=auto', s)
        self.assertIn('src.backup(dst)', s)
        self.assertIn('PRAGMA integrity_check', s)
        self.assertIn("--exclude='var/lib/mbox/users.db'", s)
        self.assertIn('/etc/systemd/system/mbox-quic-staging.service', s)
        self.assertIn('/etc/mbox/quic-caddy', s)
        self.assertIn('systemctl restart mbox-server.service', s)
        self.assertNotIn('systemctl enable --now mbox-native-quic', s)
        self.assertNotIn('systemctl enable --now mbox-quic-renew.timer', s)

    def test_admin_branch_is_before_server_build_and_restart(self):
        s = INSTALL.read_text()
        self.assertLess(s.index('if (( WITH_ADMIN )); then'), s.index('go build -trimpath'))
        self.assertIn('python3 "$ROOT/tools/mboxctl/mboxctl.py" config validate', s)
        self.assertIn('systemctl restart mbox-admin.service', s)
        self.assertNotIn('systemctl enable --now mbox-server.service', s)
        self.assertIn('RuntimeDirectoryMode=0700', ADMIN_UNIT.read_text())
        self.assertIn('/run/mbox-admin/admin.sock', ADMIN_UNIT.read_text())
        self.assertIn("getboolean('quic', 'enabled'", ADMIN_PY.read_text())


if __name__ == '__main__':
    unittest.main()
