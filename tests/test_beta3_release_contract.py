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
    assert (ROOT/'VERSION').read_text().strip()=='1.0.0-beta.3'
    for filename in ('README.md','README.ru.md','RELEASE_NOTES.md','RELEASE_NOTES.ru.md',
                     'RELEASE_CHECKLIST.md','RELEASE_CHECKLIST.ru.md','CHANGELOG.md',
                     'CHANGELOG.ru.md','docs/STATUS.md','docs/ru/STATUS.md'):
        assert '1.0.0-beta.3' in (ROOT/filename).read_text(), filename
