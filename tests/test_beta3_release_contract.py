"""Guard documentation and release-artifact boundaries in the beta.3 rename."""
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]

def test_release_does_not_package_debug_apk():
    script=(ROOT/'tools/build-release-apk.sh').read_text()
    assert 'assembleRelease' in script
    assert 'app-release-unsigned.apk' in script
    assert 'check-production-apk.sh' in script
    assert 'MBOX_RELEASE_KEYSTORE' in script
    assert 'MBOX_RELEASE_SIGNER_SHA256' in script
    assert 'app-debug.apk' not in script

def test_release_notes_and_readmes_are_consistent():
    assert (ROOT/'VERSION').read_text().strip() == '1.0.0-beta.4'
    # Historical beta.3 notes remain historical; the new status docs are the
    # authoritative description of beta.4 until a signed release is published.
    for filename in ('docs/BETA4_RELEASE_STATUS.md',
                     'docs/ru/BETA4_RELEASE_STATUS.md'):
        assert '1.0.0-beta.4' in (ROOT / filename).read_text(), filename
    for filename, target in (
        ('README.md', 'docs/BETA4_RELEASE_STATUS.md'),
        ('README.ru.md', 'docs/ru/BETA4_RELEASE_STATUS.md'),
    ):
        text = (ROOT / filename).read_text()
        assert f']({target})' in text, filename
