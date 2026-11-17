"""Guard beta.4.3 beginner/release doc links and release identity."""
from pathlib import Path
import importlib.util

ROOT = Path(__file__).resolve().parents[1]


def test_release_docs10_version_and_paired_entrypoints():
    assert (ROOT / 'VERSION').read_text().strip() == '1.0.0-beta.4.3'
    for part in ('README', 'RELEASE_NOTES', 'RELEASE_CHECKLIST', 'CHANGELOG'):
        a = (ROOT / f'{part}.md').read_text()
        b = (ROOT / f'{part}.ru.md').read_text()
        assert '1.0.0-beta.4.3' in a and '1.0.0-beta.4.3' in b
    for p in ('docs/GETTING_STARTED.md', 'docs/ru/GETTING_STARTED.md',
              'docs/SERVER_QUICKSTART.md', 'docs/ru/SERVER_QUICKSTART.md'):
        assert (ROOT / p).is_file()


def test_release_docs10_does_not_claim_completed_metrics():
    en = (ROOT / 'RELEASE_NOTES.md').read_text()
    ru = (ROOT / 'RELEASE_NOTES.ru.md').read_text()
    assert 'not integrated' in en
    assert 'не подключён' in ru
    assert 'versionCode=10' in en and 'versionCode=10' in ru


def test_release_docs10_checker_is_read_only_and_passes():
    path = ROOT / 'tools/check-release-docs.py'
    spec = importlib.util.spec_from_file_location('beta43_docs_checker', path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    assert mod.check() == 0
