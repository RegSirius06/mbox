"""The distributed Android version and tooling must agree for staged updates."""
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]


def test_beta3_version_matches_android_and_mboxctl():
    version = (ROOT / 'VERSION').read_text().strip()
    assert version == '1.0.0-beta.3'
    gradle = (ROOT / 'android/app/build.gradle').read_text()
    assert re.search(r"\bversionName\s+'1\.0\.0-beta\.3'", gradle)
    assert re.search(r'\bversionCode\s+6\b', gradle)
    cli = (ROOT / 'tools/mboxctl/mboxctl.py').read_text()
    assert 'mboxctl 1.0.0-beta.3' in cli
