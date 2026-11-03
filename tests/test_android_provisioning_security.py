from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PROFILE = (ROOT / "android/app/src/main/java/io/mbox/android/ProfileStore.java").read_text()
ACTIVITY = (ROOT / "android/app/src/main/java/io/mbox/android/MainActivity.java").read_text()


def test_query_profile_secret_is_rejected():
    assert 'getQueryParameter("profile")!=null' in PROFILE
    assert 'legacy provisioning' in PROFILE


def test_fragment_profile_remains_supported():
    assert 'getEncodedFragment()' in PROFILE
    assert 'fragment.invalid' in PROFILE


def test_dangerous_remote_options_require_second_explicit_consent():
    for token in ('android_http_stack=go', 'network_debug=true', 'deterministic_transport=true'):
        assert token in PROFILE
    assert 'Review unsafe options' in ACTIVITY
    assert 'Import unsafe profile' in ACTIVITY
