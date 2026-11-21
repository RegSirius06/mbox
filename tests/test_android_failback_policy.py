"""Host regression for real policy plus source boundaries; not an Android leak audit."""
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT / "android/app/src/main/java/io/mbox/android"


def test_real_failback_policy(tmp_path):
    assert shutil.which("javac") and shutil.which("java"), "CI JDK required"
    subprocess.run(["javac", "--release", "17", "-d", str(tmp_path),
                    str(JAVA / "TransportPolicy.java"),
                    str(ROOT / "tests/TransportFailbackHarness.java")], check=True, timeout=30)
    p = subprocess.run(["java", "-cp", str(tmp_path), "io.mbox.android.TransportFailbackHarness"],
                       capture_output=True, text=True, check=True, timeout=10)
    assert "TransportFailback OK" in p.stdout


def test_failback_boundary_contract():
    service = (JAVA / "MBoxVpnService.java").read_text()
    probe = (JAVA / "QuicFailback.java").read_text()
    bridge = (JAVA / "QuicLabTransport.java").read_text()
    native = (ROOT / "native-quic/main.go").read_text()
    assert "try (QuicFailback failback =" in service
    transition = service.split('if (plannedFailback &&', 1)[1].split('continue;', 1)[0]
    assert "cleanupCoreTransport();" in transition and "cleanupAllTransport" not in transition
    assert "startCoreTransport" not in probe
    assert "QuicLabTransport.startProbe(context, network)" in probe
    assert "power.isDeviceIdleMode()" in probe
    assert "Thread.currentThread().isInterrupted()" in probe
    assert "probe.close()" in probe
    assert "builder.command().add(\"--probe\")" in bridge
    assert "new QuicSocketProtector(network)" in bridge
    assert native.index('requireNumericRemoteOnAndroid(remoteURL.Host)') < native.index('if *probe {')
    assert native.index('quicHTTPClientWithProtection(') < native.index('if *probe {')
    assert 'context.WithTimeout(context.Background(), 6*time.Second)' in native
