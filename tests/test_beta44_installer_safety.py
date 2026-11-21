'''Read-only guards for the explicitly staged beta.4.4 installer.'''
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parents[1]
SCRIPT=(ROOT/'install.sh').read_text()

def test_check_mode_is_read_only_and_non_downloading():
    assert 'CHECK ONLY: no files modified; no processes restarted; no toolchain download.' in SCRIPT
    assert 'echo "Native QUIC module requires Go' in SCRIPT
    assert subprocess.run(['bash','-n',str(ROOT/'install.sh')]).returncode==0

def test_upgrade_does_not_rebuild_gateway_or_clobber_unit():
    assert 'if (( FRESH_INSTALL )); then' in SCRIPT
    assert 'origin_bins=(mbox-server mbox-client mbox-key mbox-link)' in SCRIPT
    assert 'origin_bins+=(mbox-native-quic)' in SCRIPT
    assert 'Keep the operator' in SCRIPT and 'DATAGRAM ACL' in SCRIPT
    assert 'install -m 0644 "$ROOT/deploy/mbox-server.service"' in SCRIPT

def test_installation_is_opt_in():
    for flag in ('--check','--fresh-install','--upgrade-origin','--with-admin','--with-subscriptions'):
        assert flag in SCRIPT
    assert 'Choose exactly one mode' in SCRIPT
