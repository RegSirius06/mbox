"""Guard the minimum beta.4.1 fail-closed health/lifecycle wiring."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
GO = ROOT / 'client'
JAVA = ROOT / 'android/app/src/main/java/io/mbox/android'


def test_existing_noise_ping_echo_without_wire_revision():
    assert 'case framePing:' in (ROOT/'server/streams.go').read_text()
    assert 'return appendFrame(s, streamID, framePing, payload)' in (ROOT/'server/streams.go').read_text()
    client = (GO/'health.go').read_text()
    assert 't.queue(0, framePing, nonce[:])' in client
    assert 'subtle.ConstantTimeCompare' in client
    assert 'remote authenticated ping timed out' in client
    assert 'case framePing:' in (GO/'streams.go').read_text()
    assert 't.acceptHealthPong(id, payload)' in (GO/'streams.go').read_text()


def test_android_only_connected_after_remote_proof():
    service = (JAVA/'MBoxVpnService.java').read_text()
    assert 'CONNECTING, VERIFYING, CONNECTED, RECOVERING' in service
    assert 'waitForRemoteVerification(process, remoteVerified);' in service
    assert service.index('waitForRemoteVerification(process, remoteVerified);') < service.index('return process;')
    assert 'quicLabTransport.isAlive()' in service
    assert 'TProxyService.TProxyIsRunning()' in service
    assert 'OsConstants.SIGUSR1' in service
    assert 'Intent.ACTION_SCREEN_ON' in service
    assert 'ACTION_DEVICE_IDLE_MODE_CHANGED' in service
    assert 'if (!desiredRunning) throw new InterruptedException' in service
    assert 'ensureFailClosedVpnInterface();' in service
    assert 'cleanupAllTransport();' in service


def test_version_and_app_id():
    build = (ROOT/'android/app/build.gradle').read_text()
    assert "applicationId 'io.mbox.android'" in build
    assert "versionCode 8" in build
    assert "versionName '1.0.0-beta.4.1'" in build
