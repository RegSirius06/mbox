"""Run the production Java auth gate and catch auth-after-body regressions."""
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
BRIDGE = ROOT / "android/app/src/main/java/io/mbox/android/CronetBridge.java"
PROFILE = ROOT / "android/app/src/main/java/io/mbox/android/ProfileStore.java"
GATE = ROOT / "android/app/src/main/java/io/mbox/android/BridgeAuthGate.java"
HARNESS = ROOT / "tests/BridgeAuthGateHarness.java"


def test_bridge_authentication_precedes_large_body_parsing():
    source = BRIDGE.read_text()
    reader = source.split("private BridgeRequest readAuthenticatedRequest(", 1)[1]
    reader = reader.split("private BridgeResult execute(", 1)[0]
    assert reader.index("BridgeAuthGate.requireToken(in, REQUEST_MAGIC, token)") < reader.index("readString32(in, 64 * 1024)")
    assert reader.index("BridgeAuthGate.requireToken(in, REQUEST_MAGIC, token)") < reader.index("new byte[(int) bodyLength]")
    assert "constantTimeEquals(token, request.token)" not in source
    assert "catch (BridgeAuthGate.InvalidTokenException ignored)" in source


def test_profile_import_limits_untrusted_deep_link_size():
    source = PROFILE.read_text()
    assert "MAX_LINK_CHARS = 64 * 1024" in source
    assert "MAX_PROFILE_BYTES = 48 * 1024" in source
    assert "input.length()>MAX_LINK_CHARS" in source
    assert "decoded.length>MAX_PROFILE_BYTES" in source


def test_real_java_gate_parser(tmp_path):
    assert shutil.which("javac") and shutil.which("java"), "host JDK required for regression"
    subprocess.run(["javac", "-d", str(tmp_path), str(GATE), str(HARNESS)],
                   check=True, timeout=60)
    result = subprocess.run(["java", "-cp", str(tmp_path), "io.mbox.android.BridgeAuthGateHarness"],
                            capture_output=True, text=True, check=True, timeout=60)
    assert "PASS: Cronet bridge pre-auth gate" in result.stdout
