"""Android UX/source safety checks; on-device behaviour still requires verification."""
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT / 'android/app/src/main/java/io/mbox/android'
MAIN = (JAVA / 'MainActivity.java').read_text()
SERVICE = (JAVA / 'MBoxVpnService.java').read_text()
UPDATES = (JAVA / 'ReleaseUpdates.java').read_text()


def test_settings_clipboard_first_and_manual_import_fourth():
    settings = MAIN.split('private void showSettingsHome() {', 1)[1].split('private void showProfileHub() {', 1)[0]
    items = [settings.index('this::importFromClipboard'), settings.index('this::showProfileHub'),
             settings.index('this::showLanguagePage'), settings.index('this::showImportDialog')]
    assert items == sorted(items)
    assert 'MBOX_BETA43_RELEASE_UPDATES_FINAL' in settings


def test_github_link_visible_on_home_and_release_check_vpn_only():
    assert 'releases.setOnClickListener(v -> openGithubReleases())' in MAIN
    assert 'ReleaseUpdates.maybeCheck(this)' in MAIN
    assert 'caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)' in UPDATES
    assert 'vpn.openConnection(new URL(API))' in UPDATES
    assert 'getState() != MBoxVpnService.State.CONNECTED' in UPDATES
    assert 'https://api.github.com/repos/RegSirius06/mbox/releases?per_page=5' in UPDATES
    assert 'https://github.com/RegSirius06/mbox/releases' in UPDATES
    assert 'setInstanceFollowRedirects(false)' in UPDATES
    assert 'MAX_BYTES = 256 * 1024' in UPDATES
    assert 'INTERVAL_MS = 6 * 60 * 60 * 1000L' in UPDATES
    assert 'prefs.getString("last_notified", "")' in UPDATES
    assert 'release.optBoolean("draft", true)' in UPDATES
    assert 'addAction(android.R.drawable.ic_menu_view' in UPDATES
    assert 'ru ? "Остаться в MBox"' in UPDATES
    assert 'ru ? "Открыть GitHub"' in UPDATES
    assert 'ReleaseUpdates.dismiss(this)' in MAIN
    assert 'ReleaseUpdates.ACTION_STAY' in MAIN
    assert 'ReleaseUpdates.ACTION_GITHUB' in MAIN


def test_foreground_service_stop_action_on_both_notifications():
    assert 'private PendingIntent notificationStopIntent()' in SERVICE
    assert 'new Intent(this, MBoxVpnService.class).setAction(ACTION_STOP)' in SERVICE
    assert SERVICE.count('notificationStopIntent())') == 2
    assert 'if (ACTION_STOP.equals(action))' in SERVICE
    assert 'private PendingIntent notificationOpenIntent()' in SERVICE
