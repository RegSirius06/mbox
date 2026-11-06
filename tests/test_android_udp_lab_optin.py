from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT / "android/app/src/main/java/io/mbox/android"


def test_udp_lab_is_debug_only_and_not_imported_from_link():
    runtime = (JAVA / "AppRuntime.java").read_text()
    profile = (JAVA / "ProfileStore.java").read_text()
    service = (JAVA / "MBoxVpnService.java").read_text()
    assert "ApplicationInfo.FLAG_DEBUGGABLE" in runtime
    assert 'put("enable_udp_lab",AppRuntime.udpLabEnabled(c))' in profile
    assert 'put("MBOX_DEVELOPER_MODE", "1")' in service
    assert 'if (AppRuntime.udpLabEnabled(this))' in service
    # Imported link never chooses whether the client runs the UDP lab.
    assert 'dt.put("enable_udp_lab"' not in profile
