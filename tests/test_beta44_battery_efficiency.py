"""Regression guards for battery changes over the on-demand rekey baseline."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ANDROID = ROOT / "android/app/src/main/java/io/mbox/android"


def test_datagram_starts_after_first_real_udp_packet():
    source = (ROOT / "client/udp_flows.go").read_text()
    before, after = source.split("reader := bufio.NewReaderSize(conn, 4096)", 1)
    assert "t.startDatagramLane()" not in before
    assert "t.startDatagramLane()" in after
    assert "t.datagramLane.Load().Offer(id, wire)" in after
    assert "t.queue(id, frameUDPData, wire)" in after
    assert "KeepAlivePeriod" not in (ROOT / "native-quic/datagram_lane.go").read_text()


def test_failback_only_queries_network_for_due_probe():
    supervisor = (ANDROID / "MBoxVpnService.java").read_text()
    failback = (ANDROID / "QuicFailback.java").read_text()
    assert "failback.tick(networkMonitor, connectedAt)" in supervisor
    assert "process.waitFor(3, TimeUnit.SECONDS)" in supervisor
    assert "if (probe == null && !policy.probeDue(" in failback
    assert failback.index("!policy.probeDue(") < failback.index("power.isDeviceIdleMode()")
    assert "monitor.currentNetwork()" in failback


def test_wake_probe_dedup_and_foreground_ui_refresh():
    service = (ANDROID / "MBoxVpnService.java").read_text()
    activity = (ANDROID / "MainActivity.java").read_text()
    assert "lastWakeProbeElapsed" in service
    assert "state != State.CONNECTED" in service
    assert "now - previous < 10_000L" in service
    assert "sendSignal(pid, OsConstants.SIGUSR1)" in service
    assert "handler.postDelayed(this, 1000);" in activity
    assert "handler.removeCallbacks(refreshTask);" in activity
