from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT / "android/app/src/main/java/io/mbox/android"


def test_debug_diagnostics_are_gated_but_mbox_udp_is_enabled_in_runtime():
    runtime = (JAVA / "AppRuntime.java").read_text()
    profile = (JAVA / "ProfileStore.java").read_text()
    service = (JAVA / "MBoxVpnService.java").read_text()
    assert "ApplicationInfo.FLAG_DEBUGGABLE" in runtime
    assert 'put("enable_udp",true)' in profile
    assert 'remove("enable_udp_lab")' in profile
    assert 'put("MBOX_DEVELOPER_MODE", "1")' in service
    assert 'if (AppRuntime.udpLabEnabled(this))' in service
    # Imported link cannot silently enable developer mode or legacy lab switch.
    assert 'dt.put("enable_udp_lab"' not in profile


def test_debug_apk_is_visibly_marked_as_udp_lab():
    gradle = (ROOT / "android/app/build.gradle").read_text()
    assert "versionNameSuffix '-udp-lab'" in gradle
