"""Beta.4.3 UI safety contract: real page navigation, hidden debug, tap-to-open."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ACT = (ROOT / 'android/app/src/main/java/io/mbox/android/MainActivity.java').read_text()
VPN = (ROOT / 'android/app/src/main/java/io/mbox/android/MBoxVpnService.java').read_text()


def test_settings_profiles_diagnostics_are_pages_not_popups():
    assert 'MBOX_BETA43_NATIVE_SCREENS' in ACT
    for name in ('showSettingsHome', 'showProfileHub', 'showDiagnostics'):
        src = ACT.split('private void ' + name + '() {', 1)[1].split('\n    private ', 1)[0]
        assert 'openPage(' in src, name
        assert 'new AlertDialog.Builder' not in src, name
    assert 'setContentView(scroll)' in ACT
    assert 'void onBackPressed()' in ACT


def test_debug_only_via_hidden_easter_egg():
    assert 'if (++diagnosticTaps >= 7)' in ACT
    assert 'diagnosticsView.setText(diagnosticsSnapshot())' in ACT
    assert 'tap seven times for diagnostics' not in ACT
    menu = ACT.split('private void showSettingsHome()', 1)[1].split('private void showProfileHub()', 1)[0]
    assert 'this::showDiagnostics' not in menu


def test_notification_intent_and_channel_diagnostics():
    assert 'ACTION_OPEN_STATUS' in VPN
    assert '.setAction(ACTION_OPEN_STATUS)' in VPN
    assert 'setForegroundServiceBehavior(Notification.FOREGROUND_SERVICE_IMMEDIATE)' in VPN
    for name in ('ensureForeground', 'updateNotification'):
        src = VPN.split('private void ' + name + '(String text) {', 1)[1].split('\n    private ', 1)[0]
        assert '.setContentIntent(notificationOpenIntent())' in src
        assert '.setOngoing(true)' in src
        assert '.setOnlyAlertOnce(true)' in src
    assert 'getNotificationChannelId()' in ACT
    assert 'channel.getImportance()' in ACT
    assert 'manager.getActiveNotifications()' in ACT
    assert 'MBoxVpnService.ACTION_OPEN_STATUS.equals(intent.getAction())' in ACT
