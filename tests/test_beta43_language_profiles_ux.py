"""UI regression contracts for beta.4.3; runtime/device tests are still required."""
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
SRC=ROOT/'android/app/src/main/java/io/mbox/android/'
MAIN=(SRC/'MainActivity.java').read_text()
LANG=(SRC/'UiLanguage.java').read_text()
PROFILE=(SRC/'ProfileStore.java').read_text()
SUB=(SRC/'SubscriptionStore.java').read_text()

def test_language_is_local_user_preference_and_follows_device_by_default():
    assert 'getString(KEY, "system")' in LANG
    assert 'getLocales().get(0)' in LANG
    assert '"ru".equals(selected)' in LANG
    assert '"en".equals(selected)' in LANG
    assert 'private void showLanguagePage()' in MAIN
    assert 'UiLanguage.set(this, code)' in MAIN

def test_no_public_advanced_or_manual_transport_selector():
    settings=MAIN.split('private void showSettingsHome() {',1)[1].split('private void showProfileHub() {',1)[0]
    assert 'Advanced' not in settings
    assert 'this::showLabTools' not in settings
    assert 'this::showLanguagePage' in settings
    assert 'Page.ADVANCED' not in MAIN
    assert 'if (!AppRuntime.udpLabEnabled(this)) return;' in MAIN
    assert 'this::showLabTools' in MAIN

def test_local_delete_requires_stopped_tunnel_and_checkbox():
    method=MAIN.split('private void showDeleteDirectProfile() {',1)[1].split('private String diagnosticsSnapshot()',1)[0]
    assert 'if (!vpnStopped()) return;' in method
    assert 'confirm.isChecked()' in method
    assert 'SubscriptionStore.deleteSavedDirect(this)' in method
    assert 'if (activeDirect) ProfileStore.deleteActiveProfile(this)' in method
    assert 'walkFileTree' in PROFILE
    assert 'static void deleteSavedDirect(Context c)' in SUB

def test_one_off_alerts_use_dark_theme():
    assert 'R.style.MBoxDialog' in MAIN
    assert 'new AlertDialog.Builder(this)' not in MAIN
    assert (ROOT/'android/app/src/main/res/values/styles.xml').is_file()

def test_all_subscriptions_refresh_moved_to_profiles():
    assert 'this::showSubscriptions' in MAIN
    assert 'refreshSubscription(true)' in MAIN
