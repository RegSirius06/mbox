"""Production safe selection is separate from developer-only lab switches."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ANDROID = ROOT / "android/app/src/main/java/io/mbox/android"


def test_transport_selector_is_debug_only():
    ui = (ANDROID / "MainActivity.java").read_text()
    assert 'private void showLabTools()' in ui
    assert 'if (!AppRuntime.udpLabEnabled(this)) return;' in ui
    assert 'this::showLabTools' in ui
    assert 'showTransportDialog()' in ui
    assert 'if (!AppRuntime.udpLabEnabled(this)) return;' in ui


def test_release_overrides_stale_manual_preferences():
    source = (ANDROID / "QuicLabTransport.java").read_text()
    part = source.split('static TransportPolicy.Mode mode(Context c) {', 1)[1].split('static TransportPolicy.Mode cycleMode', 1)[0]
    assert 'if (!configured(c)) return TransportPolicy.Mode.HTTPS;' in part
    assert 'if (!AppRuntime.udpLabEnabled(c)) return TransportPolicy.Mode.AUTO;' in part
    assert part.index('TransportSettings.mode(c)') < part.index('if (!AppRuntime.udpLabEnabled(c))')
    assert 'if (explicit == TransportPolicy.Mode.QUIC || explicit == TransportPolicy.Mode.HTTPS) return explicit;' in part
    assert part.index('if (!AppRuntime.udpLabEnabled(c))') < part.index('prefs.getString(MODE, null)')
    cycle = source.split('static TransportPolicy.Mode cycleMode(Context c) {', 1)[1]
    assert 'if (!AppRuntime.udpLabEnabled(c)) {' in cycle
    assert 'throw new SecurityException("Manual transport mode requires debug build")' in cycle


def test_production_settings_are_hidden_and_do_not_enable_lab():
    ui = (ANDROID / "MainActivity.java").read_text()
    diagnostics = ui.split('private void showDiagnostics()', 1)[1].split('private void showLabTools()', 1)[0]
    assert 'TransportSettings.show(this)' in diagnostics
    assert diagnostics.index('TransportSettings.show(this)') < diagnostics.index('if (AppRuntime.udpLabEnabled(this))')
    settings = (ANDROID / "TransportSettings.java").read_text()
    assert '"mbox-transport-settings"' in settings and '"mode-v2"' in settings
    assert 'State.STOPPED' in settings and 'if (!stopped(a))' in settings
    assert 'enable_udp_lab' not in settings and 'MBOX_DEVELOPER_MODE' not in settings


def test_vpn_service_observes_effective_mode():
    service = (ANDROID / "MBoxVpnService.java").read_text()
    assert 'TransportPolicy.Mode requestedMode = QuicLabTransport.mode(this);' in service
    runtime = (ANDROID / "AppRuntime.java").read_text()
    assert 'ApplicationInfo.FLAG_DEBUGGABLE' in runtime
