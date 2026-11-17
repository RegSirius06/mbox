"""Only static contract checks. Real Android Gradle/device checks are required."""
from pathlib import Path
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
JAVA = ROOT/'android/app/src/main/java/io/mbox/android/MainActivity.java'
RES = ROOT/'android/app/src/main/res'
ANDROID = '{http://schemas.android.com/apk/res/android}'


def test_theme_follows_ui_mode_and_no_fixed_light_theme():
    manifest = ET.parse(ROOT/'android/app/src/main/AndroidManifest.xml').getroot()
    app = manifest.find('application')
    assert app.attrib[ANDROID+'theme'] == '@style/MBoxTheme'
    assert app.attrib[ANDROID+'icon'] == '@mipmap/ic_launcher'
    light = (RES/'values/styles.xml').read_text()
    dark = (RES/'values-night/styles.xml').read_text()
    assert '@android:style/Theme.Material.Light.NoActionBar' in light
    assert '@android:style/Theme.Material.NoActionBar' in dark
    assert 'MBoxDialog' in light and 'MBoxDialog' in dark
    assert 'true' in light and 'false' in dark


def test_android_custom_ui_palette_has_day_night_definitions():
    source = JAVA.read_text()
    assert 'MBOX_BETA43_THEME_BRAND8' in source
    assert 'scroll.setBackgroundColor(getColor(R.color.mbox_bg));' in source
    assert 'getColor(R.color.mbox_text)' in source
    assert 'getColor(R.color.mbox_surface)' in source
    day = ET.parse(RES/'values/colors.xml').getroot()
    night = ET.parse(RES/'values-night/colors.xml').getroot()
    dc={item.get('name'):item.text for item in day}
    nc={item.get('name'):item.text for item in night}
    assert dc.keys() == nc.keys()
    assert dc['mbox_bg'] != nc['mbox_bg']
    assert dc['mbox_text'] != nc['mbox_text']
    assert 'R.color.mbox_on_danger' in source


def test_user_supplied_mailbox_brand_icons_bundled():
    icon = RES/'drawable-nodpi/mbox_launcher_art.png'
    assert icon.read_bytes().startswith(b'\x89PNG\r\n\x1a\n')
    for density in ('mdpi','hdpi','xhdpi','xxhdpi','xxxhdpi'):
        assert (RES/f'mipmap-{density}/ic_launcher.png').read_bytes().startswith(b'\x89PNG')
    root = ET.parse(RES/'mipmap-anydpi-v26/ic_launcher.xml').getroot()
    assert root.tag == 'adaptive-icon'
    assert root.find('foreground').attrib[ANDROID+'drawable'] == '@drawable/mbox_launcher_art'


def test_reg_ru_cpu_not_faked_and_version_is_current_ui():
    html=(ROOT/'tools/mbox-admin/index.html').read_text()
    assert 'MBOX_BETA43_THEME_BRAND8' in html
    assert 'beta.4.2' not in html
    assert 'Нет интеграции с REG.RU' in html
    assert 'Автоматического опроса нет.' in html
    assert 'REG.RU · CPU / ресурсы' in html
    assert 'mboxAdminSelectTab' in html
