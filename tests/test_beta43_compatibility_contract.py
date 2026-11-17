"""beta.4.3 metadata and cosmetic-only constraints; real device tests still required."""
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]

def test_all_version_gates_agree():
    assert (ROOT/'VERSION').read_text().strip() == '1.0.0-beta.4.3'
    gradle=(ROOT/'android/app/build.gradle').read_text()
    assert "versionName '1.0.0-beta.4.3'" in gradle
    assert 'versionCode 10' in gradle
    assert "mboxctl 1.0.0-beta.4.3" in (ROOT/'tools/mboxctl/mboxctl.py').read_text()
    assert '1.0.0-beta.4.3' in (ROOT/'tools/build-release-apk.sh').read_text()
    gate=(ROOT/'tools/check-production-apk.sh').read_text()
    assert "versionCode='10' versionName='1.0.0-beta.4.3'" in gate

def test_ui_stays_cosmetic():
    ui=(ROOT/'android/app/src/main/java/io/mbox/android/MainActivity.java').read_text()
    assert 'MBOX_COMPACT_HOME_V2' in ui
    assert 'QuicLabTransport.mode(this)' in ui
    assert 'ProfileStore.hasProfile(this)' in ui
    assert 'SubscriptionStore.importUrl(this, url)' in ui
    assert 'MBoxVpnService.State.CONNECTED' in ui
    assert 'connectButton.setVisibility(running ? View.GONE : View.VISIBLE)' in ui

def test_separate_languages_and_prerelease():
    for path in ('RELEASE_NOTES.md','RELEASE_NOTES.ru.md', 'CHANGELOG.md','CHANGELOG.ru.md'):
        t=(ROOT/path).read_text()
        assert 'beta.4.3' in t
    assert 'not RC1' in (ROOT/'docs/BETA4_3_STATUS.md').read_text()
    assert 'не RC1' in (ROOT/'docs/ru/BETA4_3_STATUS.md').read_text()
