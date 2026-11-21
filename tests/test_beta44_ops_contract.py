from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_fastcgi_watchdog_wraps_complete_request_handler():
    main = (ROOT / "relay/main.go").read_text()
    guard = (ROOT / "relay/fcgi_guard.go").read_text()
    assert "guardedHandler := fcgiWatchdog(handler, fcgiHardLimit" in main
    assert "serveGuardedFastCGI(guardedHandler)" in main
    assert "guardRelayProxy" not in main
    assert "complete application handler" in guard


def test_datagram_mtu_failure_retires_lane_for_stream_fallback():
    source = (ROOT / "native-quic/datagram_lane.go").read_text()
    branch = source.split("var tooLarge *quic.DatagramTooLargeError", 1)[1].split("\n\t\t\t}", 1)[0]
    assert "return" in branch
    assert "continue" not in branch
