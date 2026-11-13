"""Production MBox must never expose or honor manual transport selection."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ANDROID = ROOT / "android/app/src/main/java/io/mbox/android"


def test_transport_selector_is_debug_only():
    ui = (ANDROID / "MainActivity.java").read_text()
    assert "final boolean manualTransport = debugLab && hasQuic;" in ui
    assert 'if (manualTransport) options.add("Transport mode");' in ui
    assert 'else if (choice == 2 && manualTransport) showTransportDialog();' in ui
    assert 'if (!AppRuntime.udpLabEnabled(this)) return;' in ui


def test_release_overrides_stale_manual_preferences():
    source = (ANDROID / "QuicLabTransport.java").read_text()
    part = source.split('static TransportPolicy.Mode mode(Context c) {', 1)[1].split('static TransportPolicy.Mode cycleMode', 1)[0]
    assert 'if (!configured(c)) return TransportPolicy.Mode.HTTPS;' in part
    assert 'if (!AppRuntime.udpLabEnabled(c)) return TransportPolicy.Mode.AUTO;' in part
    assert part.index('if (!AppRuntime.udpLabEnabled(c))') < part.index('prefs.getString(MODE, null)')
    cycle = source.split('static TransportPolicy.Mode cycleMode(Context c) {', 1)[1]
    assert 'if (!AppRuntime.udpLabEnabled(c)) {' in cycle
    assert 'throw new SecurityException("Manual transport mode requires debug build")' in cycle


def test_vpn_service_observes_effective_mode():
    service = (ANDROID / "MBoxVpnService.java").read_text()
    assert 'TransportPolicy.Mode requestedMode = QuicLabTransport.mode(this);' in service
    runtime = (ANDROID / "AppRuntime.java").read_text()
    assert 'ApplicationInfo.FLAG_DEBUGGABLE' in runtime
