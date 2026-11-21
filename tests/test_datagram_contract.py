"""Regression guards for automatic QUIC-only DATAGRAM (not network tests)."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT / 'android/app/src/main/java/io/mbox/android'


def test_datagram_is_automatic_and_quic_only():
    settings = (JAVA / 'TransportSettings.java').read_text()
    service = (JAVA / 'MBoxVpnService.java').read_text()
    gateway = (JAVA / 'QuicLabTransport.java').read_text()
    assert 'static boolean datagrams(Context c) { return QuicLabTransport.configured(c); }' in settings
    assert 'DATAGRAM (experimental)' not in settings
    assert 'if (!probeOnly && TransportSettings.datagrams(context))' in gateway
    block = service.split('if (quicLabTransport != null) {\n                processBuilder.environment()', 1)[1].split('if (cronetBridge != null)', 1)[0]
    assert 'MBOX_QUIC_UNIX' in block and 'MBOX_DATAGRAM' in block
    src = (ROOT/'client/datagram_lane.go').read_text()
    assert 'os.Getenv("MBOX_QUIC_UNIX")' in src
    assert 'net.DialTimeout("unix", path' in src
    assert 'MBOX_HTTP_BRIDGE_ADDR' in src
    assert 't.datagramRunning' in src
    assert 'dgram.AttachEpoch' in src
    assert 't.startDatagramLane()' in (ROOT/'client/udp_flows.go').read_text()
    assert 'KeepAlivePeriod' not in (ROOT/'native-quic/datagram_lane.go').read_text()


def test_datagram_keeps_stream_control_and_socks_guards():
    client = (ROOT/'client/udp_flows.go').read_text()
    server = (ROOT/'server/udp_flows.go').read_text()
    assert 't.queue(id, frameUDPOpen, nil)' in client
    assert 'if t.datagramLane.Load().Offer(id, wire)' in client
    assert 'if s.datagramLane.Load().Offer(id, out)' in server
    assert 'publicUDPDestination(d.Peer)' in server
    origin = (ROOT/'server/datagram_lane.go').read_text()
    assert 'dgram.VerifyProof(up, sid, "attach", request[32:])' in origin
    assert 's.datagramUsed = true' in origin
    assert 'dgram.ParseAttachEpoch' in origin
    assert 'epoch <= s.datagramEpoch' in origin
    assert 'onUDPData(s, d.FlowID, raw)' in origin
    gateway = (ROOT/'native-quic/datagram_lane.go').read_text()
    assert 'settings.EnableDatagrams' in gateway
    assert 'settings.EnableExtendedConnect' in gateway
    assert 'runtime.GOOS == "android" && dial == nil' in gateway
    assert 'SendDatagram' in gateway and 'ReceiveDatagram' in gateway
    assert 'InsecureSkipVerify' not in gateway
