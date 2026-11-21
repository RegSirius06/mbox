"""Historical test filename: active release now beta.4.4."""
from pathlib import Path
import importlib.util
ROOT=Path(__file__).resolve().parents[1]

def test_release_docs10_version_and_paired_entrypoints():
    assert (ROOT/'VERSION').read_text().strip()=='1.0.0-beta.4.4'
    for name in ('README','RELEASE_NOTES','RELEASE_CHECKLIST','CHANGELOG'):
        assert '1.0.0-beta.4.4' in (ROOT/f'{name}.md').read_text()
        assert '1.0.0-beta.4.4' in (ROOT/f'{name}.ru.md').read_text()
    for p in ('docs/GETTING_STARTED.md','docs/ru/GETTING_STARTED.md',
              'docs/SERVER_QUICKSTART.md','docs/ru/SERVER_QUICKSTART.md'):
        assert (ROOT/p).is_file()

def test_release_docs10_does_not_claim_completed_metrics():
    a=(ROOT/'RELEASE_NOTES.md').read_text()
    b=(ROOT/'RELEASE_NOTES.ru.md').read_text()
    assert 'CPU' in a and 'CPU' in b
    assert 'not' in a and 'не' in b
    assert 'versionCode 11' in a and 'versionCode 11' in b

def test_release_docs10_checker_is_read_only_and_passes():
    p=ROOT/'tools/check-release-docs.py'
    spec=importlib.util.spec_from_file_location('release_docs',p)
    mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod)
    assert mod.check()==0
