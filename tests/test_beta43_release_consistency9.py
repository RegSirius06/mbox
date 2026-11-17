"""Release UX9 regression checks. Not a substitute for Android/device/production tests."""
from pathlib import Path
import subprocess
import xml.etree.ElementTree as ET

ROOT=Path(__file__).resolve().parents[1]
MAIN=(ROOT/'android/app/src/main/java/io/mbox/android/MainActivity.java').read_text()
INSTALL=(ROOT/'install.sh').read_text()
RES=ROOT/'android/app/src/main/res'

def _luminance(color):
    vals=[int(color.lstrip('#')[i:i+2],16)/255 for i in (0,2,4)]
    rs=[v/12.92 if v<=0.04045 else ((v+0.055)/1.055)**2.4 for v in vals]
    return sum(a*b for a,b in zip(rs,(0.2126,0.7152,0.0722)))

def _ratio(a,b):
    x,y=sorted((_luminance(a),_luminance(b)))
    return (y+0.05)/(x+0.05)

def test_buttons_have_opaque_high_contrast_colors_in_both_modes():
    for directory in ('values','values-night'):
        parsed=ET.parse(RES/directory/'colors.xml').getroot()
        colors={e.get('name'):(e.text or '').strip() for e in parsed if e.tag=='color'}
        for background,foreground in [('mbox_primary_bg','mbox_on_primary_bg'),
                                      ('mbox_stop_bg','mbox_on_stop_bg')]:
            assert _ratio(colors[background],colors[foreground])>=4.5
    assert 'primaryBackground.setColor(getColor(R.color.mbox_primary_bg));' in MAIN
    assert 'connectButton.setTextColor(getColor(R.color.mbox_on_primary_bg));' in MAIN
    assert 'stopBackground.setColor(getColor(R.color.mbox_stop_bg));' in MAIN
    assert 'disconnectButton.setTextColor(getColor(R.color.mbox_on_stop_bg));' in MAIN
    assert 'connectButton.setBackgroundTintList(null);' in MAIN
    assert 'disconnectButton.setBackgroundTintList(null);' in MAIN

def test_installer_interactive_explicit_while_staging_safe():
    assert 'MBOX_BETA43_RELEASE_CONSISTENCY9' in INSTALL
    assert 'if (( $# == 0 )) && [[ -t 0 && -t 1 ]]' in INSTALL
    assert 'set -- --fresh-install' in INSTALL
    assert 'set -- --with-admin' in INSTALL
    assert 'set -- --upgrade-origin' in INSTALL
    assert '[[ "$confirm" == UPGRADE ]]' in INSTALL
    assert 'if (( FRESH_INSTALL )) && [[ -t 0 ]]' in INSTALL
    assert 'bash "$ROOT/tools/relay-setup.sh"' in INSTALL
    assert 'No changes.' in INSTALL
    assert subprocess.run(['bash','-n',str(ROOT/'install.sh')]).returncode==0
    wizard=ROOT/'tools/relay-setup.sh'
    assert wizard.is_file()
    content=wizard.read_text()
    assert 'Type DEPLOY' in content
    assert 'mboxctl "${args[@]}" --dry-run' in content
    assert subprocess.run(['bash','-n',str(wizard)]).returncode==0

def test_release_metadata_agree():
    version=(ROOT/'VERSION').read_text().strip()
    assert version=='1.0.0-beta.4.3'
    assert 'mboxctl '+version in (ROOT/'tools/mboxctl/mboxctl.py').read_text()
    gradle=(ROOT/'android/app/build.gradle').read_text()
    assert "versionName '1.0.0-beta.4.3'" in gradle
    assert 'versionCode 10' in gradle
