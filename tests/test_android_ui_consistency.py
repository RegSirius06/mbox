"""Prevent the debug/release UI split and silent profile import errors."""
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
SOURCE=ROOT/'android/app/src/main/java/io/mbox/android/MainActivity.java'

def test_same_simple_ui_in_debug_and_release():
    source=SOURCE.read_text()
    assert 'compactUi = true;' in source
    assert 'FLAG_DEBUGGABLE) == 0' not in source
    assert 'if (debugLab) options.add("UDP Lab: STUN through VPN")' in source
    assert 'AppRuntime.udpLabEnabled(this)' in source

def test_profile_import_failures_are_visible():
    source=SOURCE.read_text()
    assert 'showImportError(error)' in source
    assert 'Profile import failed' in source
    assert 'MBoxLogSanitizer.sanitize(explanation)' in source
