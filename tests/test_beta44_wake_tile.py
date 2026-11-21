'Static Android/QS invariants; no emulator required.'
from pathlib import Path
import subprocess
import tempfile
import shutil
import re

ROOT = Path(__file__).resolve().parents[1]
BASE = ROOT / 'android/app/src/main'


def test_tile_manifest_contract():
    manifest = (BASE/'AndroidManifest.xml').read_text()
    assert 'android.permission.BIND_QUICK_SETTINGS_TILE' in manifest
    assert 'android.service.quicksettings.action.QS_TILE' in manifest
    assert 'android.service.quicksettings.ACTIVE_TILE' in manifest
    assert 'android:name=".MBoxTileService"' in manifest
    assert 'android:exported="true"' in manifest
    assert (BASE/'res/drawable/ic_mbox_tile.xml').is_file()


def test_tile_one_service_one_vpn_and_user_gesture():
    code = (BASE/'java/io/mbox/android/MBoxTileService.java').read_text()
    assert 'extends TileService' in code
    assert 'MBoxVpnService.ACTION_START' in code
    assert 'MBoxVpnService.ACTION_STOP' in code
    assert 'VpnService.prepare(this)' in code
    assert 'startActivityAndCollapse(pending)' in code
    assert 'startForegroundService(' in code
    assert 'new ComponentName(context, MBoxTileService.class)' in code
    assert 'Thread.sleep' not in code
    assert 'Timer(' not in code


def test_wake_signal_pid_and_heartbeat_status_contract():
    code = (BASE/'java/io/mbox/android/MBoxVpnService.java').read_text()
    assert 'line.contains(pidPrefix)' in code
    assert 'int reported = parseCorePid(line)' in code
    assert 'parseCorePid(String line)' in code
    assert 'sendSignal(pid, OsConstants.SIGUSR1)' in code
    assert 'WAKE_PROBE requested' in code
    assert 'MBoxTileService.requestRefresh(this)' in code
    assert 'updatePhase("Authenticated Noise heartbeat confirmed")' not in code


def test_pid_parser_with_prefixed_go_logs():
    if not shutil.which('javac') or not shutil.which('java'):
        return
    source = (BASE/'java/io/mbox/android/MBoxVpnService.java').read_text()
    start = source.index('    static int parseCorePid(String line) {')
    end = source.index('\n    private void readCoreLog(', start)
    function = source[start:end]
    runner = '''public class PidParserHarness {
%s
    public static void main(String[] args) {
        if (parseCorePid("2026/11/19 22:21:22 NET stage=PROCESS code=CORE_PID pid=3456") != 3456) throw new AssertionError("Go timestamp prefix");
        if (parseCorePid("NET stage=PROCESS code=CORE_PID pid=1234") != 1234) throw new AssertionError("Plain marker");
        if (parseCorePid("NET stage=PROCESS code=CORE_PID pid=oops") != 0) throw new AssertionError("Bad pid");
        if (parseCorePid("2026/11/19 22:21:22 NET stage=PROCESS code=CORE_PID pid=-9") != 0) throw new AssertionError("Negative pid");
        if (parseCorePid("MBOX_HEALTH code=OK") != 0) throw new AssertionError("Unrelated record");
    }
}
''' % function
    with tempfile.TemporaryDirectory() as d:
        path = Path(d)/'PidParserHarness.java'
        path.write_text(runner)
        subprocess.run(['javac',str(path)],check=True,capture_output=True,text=True)
        subprocess.run(['java','-cp',d,'PidParserHarness'],check=True,capture_output=True,text=True)
