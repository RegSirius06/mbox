"""Compile/run the real pure-Java pool code, not a Python reimplementation."""
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "android/app/src/main/java/io/mbox/android/BridgeWorkerPool.java"
HARNESS = ROOT / "tests/BridgeWorkerPoolHarness.java"
BRIDGE = ROOT / "android/app/src/main/java/io/mbox/android/CronetBridge.java"


def test_bridge_uses_bounded_socket_dispatcher():
    bridge = BRIDGE.read_text()
    assert "new BridgeWorkerPool(MAX_BRIDGE_WORKERS" in bridge
    assert "MAX_PENDING_BRIDGE_CONNECTIONS = 32" in bridge
    assert "workerExecutor.dispatch(socket, this::handle)" in bridge
    assert "workerExecutor.close()" in bridge
    assert "Executors.newFixedThreadPool(8" not in bridge


def test_bridge_pool_lifecycle_and_overload(tmp_path):
    assert shutil.which("javac") and shutil.which("java"), "host JDK required for security regression"
    subprocess.run(["javac", "-d", str(tmp_path), str(SOURCE), str(HARNESS)], check=True, timeout=60)
    result = subprocess.run(["java", "-cp", str(tmp_path), "io.mbox.android.BridgeWorkerPoolHarness"],
                            capture_output=True, text=True, check=True, timeout=60)
    assert "PASS: bounded Cronet loopback worker pool" in result.stdout
