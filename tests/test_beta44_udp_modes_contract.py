"""Private UDP mode selector and strict data routing contracts."""
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT / "android/app/src/main/java/io/mbox/android"

def test_hidden_selector_and_core_env():
    activity = (JAVA / "MainActivity.java").read_text()
    settings = (JAVA / "TransportSettings.java").read_text()
    service = (JAVA / "MBoxVpnService.java").read_text()
    assert 'showUdpMode(this)' in activity.split('private void showDiagnostics()', 1)[1]
    assert 'enum UdpMode { AUTO, STREAMS, DATAGRAMS }' in settings
    assert 'MBOX_UDP_MODE' in service
    assert 'udpMode(c) != UdpMode.STREAMS' in settings

def test_strict_server_no_data_frame_fallback():
    source = (ROOT / "server/streams.go").read_text()
    udp = (ROOT / "server/udp_flows.go").read_text()
    assert 'udpFlowDatagramsOnly(s, streamID)' in source
    assert 'if f.datagramOnly {' in udp
    assert 'newUDPFlowMode(s, streamID, len(payload) == 1)' in source

def test_auto_keeps_reliable_fallback():
    client = (ROOT / "client/udp_flows.go").read_text()
    assert 'if t.udpMode == "datagrams"' in client
    assert 't.queue(id, frameUDPData, wire)' in client
    assert 't.startDatagramLane()' in client
