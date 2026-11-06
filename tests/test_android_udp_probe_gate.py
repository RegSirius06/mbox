"""Guard the debug STUN probe against accidental physical-network fallback."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ANDROID = ROOT / "android/app/src/main/java/io/mbox/android"


def test_debug_udp_probe_is_pinned_to_vpn_network():
    probe = (ANDROID / "UdpLabProbe.java").read_text()
    activity = (ANDROID / "MainActivity.java").read_text()
    assert "AppRuntime.udpLabEnabled(context)" in probe
    assert "State.CONNECTED" in probe
    assert "NetworkCapabilities.TRANSPORT_VPN" in probe
    assert "vpn.getAllByName(" in probe
    assert "vpn.bindSocket(socket)" in probe
    assert "socket.send(" in probe
    assert "socket.receive(" in probe
    assert ".protect(" not in probe
    assert "bindProcessToNetwork(" not in probe
    assert "getActiveNetwork(" not in probe
    assert "UDP Lab: STUN through VPN" in activity
