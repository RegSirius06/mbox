"""Static regression gate for Android system-managed VPN lifecycle."""
from pathlib import Path
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
ANDROID_NS = "{http://schemas.android.com/apk/res/android}"
MANIFEST = ROOT / "android/app/src/main/AndroidManifest.xml"
SERVICE = ROOT / "android/app/src/main/java/io/mbox/android/MBoxVpnService.java"


def _vpn_service():
    tree = ET.parse(MANIFEST)
    services = [s for s in tree.findall("./application/service")
                if s.get(ANDROID_NS + "name") == ".MBoxVpnService"]
    assert len(services) == 1
    return services[0]


def test_system_can_discover_always_on_vpn_service():
    service = _vpn_service()
    # Make the service discoverable for OS-managed recovery; the platform-only
    # signature permission prevents other apps from controlling this service.
    assert service.get(ANDROID_NS + "exported") == "true"
    assert service.get(ANDROID_NS + "permission") == "android.permission.BIND_VPN_SERVICE"
    actions = [item.get(ANDROID_NS + "name") for item in service.findall("./intent-filter/action")]
    assert "android.net.VpnService" in actions
    assert any(x.get(ANDROID_NS + "name") == "android.net.VpnService.SUPPORTS_ALWAYS_ON"
               and x.get(ANDROID_NS + "value") == "true"
               for x in service.findall("./meta-data"))


def test_service_restart_is_sticky_and_null_intent_safe():
    source = SERVICE.read_text()
    assert "return START_STICKY;" in source
    assert "String action = intent == null ? null : intent.getAction();" in source
    assert "if (android.os.Build.VERSION.SDK_INT >= 24 && isAlwaysOn())" in source
    assert "VPN LIFECYCLE onCreate" in source
    assert "VPN LIFECYCLE onStartCommand" in source
    assert "VPN LIFECYCLE onDestroy" in source
    # Never log raw intent data or extras in the recovery traces.
    assert 'Log.i(TAG, "VPN LIFECYCLE onStartCommand kind="' in source
