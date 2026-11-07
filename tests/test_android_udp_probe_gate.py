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
    # Mapped DNS yields a fake IP in 100.64/10, which the UDP lab rejects.
    # This single diagnostic must use a documented literal STUN endpoint.
    assert "InetAddress.getByAddress(STUN_IPV4)" in probe
    assert "74, 125, (byte) 250, (byte) 129" in probe
    assert "getAllByName(" not in probe
    assert "vpn.bindSocket(socket)" in probe
    assert "socket.send(" in probe
    assert "socket.receive(" in probe
    assert ".protect(" not in probe
    assert "bindProcessToNetwork(" not in probe
    assert "getActiveNetwork(" not in probe
    assert "UDP Lab: STUN through VPN" in activity
