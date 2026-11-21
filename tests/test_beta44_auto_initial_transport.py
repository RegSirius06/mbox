from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT / "android/app/src/main/java/io/mbox/android"


def test_initial_preference_wired_to_authenticated_connection():
    settings = (JAVA / "TransportSettings.java").read_text()
    service = (JAVA / "MBoxVpnService.java").read_text()
    assert 'p.getBoolean(HTTPS_FIRST, false)' in settings
    assert 'putBoolean(HTTPS_FIRST, httpsFirst.isChecked())' in settings
    assert 'httpsFirst.setChecked(false)' in settings
    assert 'TransportSettings.configure(this, transportPolicy)' in service
    assert service.index('Process process = startCoreTransport(path);') < service.index('transportPolicy.connected(requestedMode, path, attemptNetworkGeneration, connectedAt);')


def test_verified_status_not_overwritten_by_core_log_reader():
    service = (JAVA / "MBoxVpnService.java").read_text()
    reader = service.split('private void readCoreLog(', 1)[1].split('private void waitForCore(', 1)[0]
    assert reader.count('verified.getCount() != 0 && process == mbox && desiredRunning') == 2
    health = reader.split('if (line.contains("NET stage=MBOX_HEALTH code=OK"))', 1)[1].split('continue;', 1)[0]
    assert 'verified.countDown()' in health and 'updatePhase(' not in health
    assert 'updatePhase("Encrypted remote session verified via " + path);' in service
