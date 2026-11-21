"""Current beta.4.4 docs must describe shipping code and open risks."""
from pathlib import Path
import importlib.util

ROOT=Path(__file__).resolve().parents[1]

def test_current_docs_gate():
    spec=importlib.util.spec_from_file_location('beta44_docs',ROOT/'tools/check-release-docs.py')
    mod=importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    assert mod.check()==0

def test_compatibility_and_risks_in_both_languages():
    for lang in ('','ru.'):
        root=(ROOT/f'README.{lang}md').read_text()
        notes=(ROOT/f'RELEASE_NOTES.{lang}md').read_text()
        assert 'MBD2' in root and 'v1' in root and 'v2' in root
        assert 'DATAGRAM' in root and 'STREAMS' in root
        assert 'REG.RU' in notes and ('CPU' in notes)
        assert '1.0.0-beta.4.4' in notes

def test_historical_docs_retained():
    for path in ('docs/BETA4_3_STATUS.md','docs/ru/BETA4_3_STATUS.md'):
        assert (ROOT/path).is_file()
