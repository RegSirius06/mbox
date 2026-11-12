"""Host-executable failover regression, plus Android VPN boundary checks."""
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT / "android/app/src/main/java/io/mbox/android"
SERVICE = JAVA / "MBoxVpnService.java"
QUIC = JAVA / "QuicLabTransport.java"
UI = JAVA / "MainActivity.java"


def test_transport_policy_java(tmp_path):
    assert shutil.which("javac") and shutil.which("java"), "CI JDK required"
    subprocess.run(["javac", "-d", str(tmp_path),
                    str(JAVA / "TransportPolicy.java"),
                    str(ROOT / "tests/TransportPolicyHarness.java")],
                   check=True, timeout=30)
    result = subprocess.run(["java", "-cp", str(tmp_path),
                             "io.mbox.android.TransportPolicyHarness"],
                            capture_output=True, text=True, check=True, timeout=10)
    assert "TransportPolicy OK" in result.stdout


def test_auto_transport_does_not_remove_vpn_or_bypass_https():
    src = SERVICE.read_text()
    assert src.index("ensureFailClosedVpnInterface();") < src.index("while (desiredRunning)")
    assert "TransportPolicy.Path path = transportPolicy.select" in src
    assert "immediateHttpsFallback = transportPolicy.failed" in src
    assert "if (immediateHttpsFallback)" in src
    assert "reconnectDelay = RECONNECT_MIN_MS;" in src
    assert "if (quicLabMode) quicLabTransport = new QuicLabTransport(this, underlyingNetwork);" in src
    assert "else cronetBridge = CronetBridge.start(this, endpointUrl, underlyingNetwork, networkDebug);" in src
    assert '"cronet".equals(httpStack)' in src
    assert "cleanupAllTransport();" in src
    assert "if (!desiredRunning)" in src


def test_auto_lab_modes_are_explicit_and_debug_only():
    quic = QUIC.read_text()
    ui = UI.read_text()
    assert "if (!AppRuntime.udpLabEnabled(c)) return false;" in quic
    assert "if (!configured(c)) return TransportPolicy.Mode.HTTPS;" in quic
    assert 'private static final String MODE = "mode-v1";' in quic
    assert 'putString(MODE, next.name()).commit()' in quic
    assert "TransportPolicy.Mode.AUTO" in ui
    assert "TransportPolicy.Mode.HTTPS" in ui
    assert "TransportPolicy.Mode.QUIC" in quic
    assert 'getState() != MBoxVpnService.State.STOPPED' in ui
