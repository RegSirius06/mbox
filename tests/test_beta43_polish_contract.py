"""Safety/UX regression checks for beta.4.3 polish."""
from pathlib import Path
import subprocess

ROOT=Path(__file__).resolve().parents[1]
ACTIVITY=(ROOT/'android/app/src/main/java/io/mbox/android/MainActivity.java').read_text()
SERVICE=(ROOT/'android/app/src/main/java/io/mbox/android/MBoxVpnService.java').read_text()
ADMIN=(ROOT/'tools/mbox-admin/index.html').read_text()
INSTALL=(ROOT/'install.sh').read_text()
WIZARD=ROOT/'tools/relay-setup.sh'


def test_notification_updates_retain_activity_action():
    part=SERVICE.split('private void updateNotification(String text) {',1)[1].split('private static void updateState(',1)[0]
    assert '.setContentIntent(' in part
    assert '.setOngoing(true)' in part
    assert '.setOnlyAlertOnce(true)' in part


def test_profiles_settings_diagnostics_keep_current_handlers():
    assert 'showSettingsHome()' in ACTIVITY
    assert 'showProfileHub()' in ACTIVITY
    assert 'showDiagnostics()' in ACTIVITY
    assert 'if (++diagnosticTaps >= 7)' in ACTIVITY
    assert 'showSubscriptionEndpoints()' in ACTIVITY
    assert 'showSubscriptions()' in ACTIVITY
    assert 'showImportDialog()' in ACTIVITY
    assert 'getConnectionAttempts()' in ACTIVITY
    assert 'Packets, bytes, streams: core counters not available yet' in ACTIVITY


def test_new_diagnostics_are_read_only_and_bounded():
    assert 'private static final java.util.concurrent.atomic.AtomicLong connectionAttempts' in SERVICE
    assert 'private static final java.util.concurrent.atomic.AtomicLong recoveryAttempts' in SERVICE
    assert 'SystemClock.elapsedRealtime()' in SERVICE
    assert 'lastSuccessfulConnectionElapsed' in SERVICE
    assert 'MBoxDiagnostics' not in SERVICE  # no new privileged daemon/sockets


def test_admin_preserves_api_and_no_live_regru_polling():
    assert 'mboxAdminSelectTab' in ADMIN
    assert 'data-mbox-tab="clients"' in ADMIN
    assert 'data-mbox-tab="subscriptions"' in ADMIN
    assert 'data-mbox-tab="diagnostics"' in ADMIN
    assert 'beta.4.3' in ADMIN
    assert 'REG.RU · CPU / ресурсы' in ADMIN
    assert 'Нет интеграции с REG.RU' in ADMIN
    assert 'Автоматического опроса нет.' in ADMIN
    assert "req('/api/action'" in ADMIN
    assert "X-MBox-CSRF" in ADMIN


def test_relay_wizard_does_not_auto_deploy():
    assert WIZARD.exists()
    script=WIZARD.read_text()
    assert 'Type DEPLOY' in script
    assert 'mboxctl "${args[@]}" --dry-run' in script
    assert 'mboxctl "${args[@]}"' in script
    assert 'read -r' in script
    assert 'relay-setup.sh' in INSTALL
    assert 'if (( FRESH_INSTALL )) && [[ -t 0 ]]' in INSTALL
    assert subprocess.run(['bash','-n',str(WIZARD)]).returncode == 0
    assert subprocess.run(['bash','-n',str(ROOT/'install.sh')]).returncode == 0
