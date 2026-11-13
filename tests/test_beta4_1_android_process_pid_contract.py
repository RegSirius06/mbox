"""Android uses a core-reported process PID, not Java Process.pid()."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SERVICE = ROOT / "android/app/src/main/java/io/mbox/android/MBoxVpnService.java"
CORE = ROOT / "client/main.go"


def test_wake_signal_uses_reported_pid():
    service = SERVICE.read_text()
    core = CORE.read_text()
    assert "process.pid()" not in service
    assert "sendSignal(pid, OsConstants.SIGUSR1)" in service
    assert "if (reported > 0 && process == mbox) corePid = reported;" in service
    assert "corePid = 0;" in service
    assert 'log.Printf("NET stage=PROCESS code=CORE_PID pid=%d", os.Getpid())' in core
