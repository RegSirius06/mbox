"""Fine-grained UI stages must be derived from actual connection steps."""
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[1]
ANDROID = ROOT / 'android/app/src/main/java/io/mbox/android'

def test_runtime_phases_visible_in_ui_and_notification():
    service = (ANDROID / 'MBoxVpnService.java').read_text()
    ui = (ANDROID / 'MainActivity.java').read_text()
    assert 'getConnectionPhase()' in service
    assert 'getConnectionPhase()' in ui
    for stage in ('Starting protected QUIC gateway', 'Starting Cronet HTTPS transport',
                  'Noise authenticated', 'Checking authenticated remote',
                  'QUIC unavailable; switching to HTTPS', 'Retry after'):
        assert stage in service
    assert 'updateNotification(safe)' in service
    assert 'NET stage=MBOX_HEALTH code=OK' in service


def test_background_probes_adaptive_and_no_public_endpoints():
    source = (ROOT / 'client/health.go').read_text()
    assert re.search(r'\bhealthActiveInterval\s*=\s*30\s*\*\s*time\.Second\b', source)
    assert re.search(r'\bhealthIdleInterval\s*=\s*90\s*\*\s*time\.Second\b', source)
    assert 't.activeStreams() > 0' in source
    assert 'framePing' in source
    assert 'http://' not in source and 'https://' not in source
