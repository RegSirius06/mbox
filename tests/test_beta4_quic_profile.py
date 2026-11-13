"""Optional public QUIC routing data is not bundled into an APK or legacy profile."""
import importlib.util
from pathlib import Path
import shutil
import subprocess
import pytest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('mboxctl_beta4', ROOT / 'tools/mboxctl/mboxctl.py')
mboxctl = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mboxctl)


def test_old_profiles_remain_https_only():
    assert mboxctl.quic_profile_metadata(None, None) == {}


@pytest.mark.parametrize('url', ['https://example.org:7443', 'http://127.0.0.1:7443',
    'https://127.0.0.1:7443/secret', 'https://127.0.0.1:7443/?x=y',
    'https://127.0.0.1:7443#tag', 'https://127.0.0.1:0', 'https://999.0.0.1:7443'])
def test_invalid_quic_url_rejected(url, tmp_path):
    cert = tmp_path / 'cert.pem'
    cert.write_text('-----BEGIN CERTIFICATE-----\nQUJD\n-----END CERTIFICATE-----\n')
    with pytest.raises(SystemExit):
        mboxctl.quic_profile_metadata(url, cert)


def test_quic_url_and_cert_must_appear_together(tmp_path):
    with pytest.raises(SystemExit):
        mboxctl.quic_profile_metadata('https://127.0.0.1:7443', None)
    with pytest.raises(SystemExit):
        mboxctl.quic_profile_metadata(None, tmp_path / 'cert.pem')


def test_optional_quic_metadata_contains_public_cert(tmp_path):
    if not shutil.which('openssl'):
        pytest.skip('openssl not installed')
    cert=tmp_path/'cert.pem';key=tmp_path/'key.pem'
    subprocess.run(['openssl','req','-x509','-newkey','ec','-pkeyopt',
        'ec_paramgen_curve:P-256','-nodes','-days','2','-keyout',str(key),
        '-out',str(cert),'-subj','/CN=test-quic',
        '-addext','subjectAltName=IP:127.0.0.1'],check=True,capture_output=True)
    result=mboxctl.quic_profile_metadata('https://127.0.0.1:7443',str(cert))
    assert result['quic_url']=='https://127.0.0.1:7443'
    assert 'BEGIN CERTIFICATE' in result['quic_ca_pem']
    assert 'PRIVATE KEY' not in result['quic_ca_pem']


def test_per_profile_quic_wiring_is_release_specific():
    java=(ROOT/'android/app/src/main/java/io/mbox/android/ProfileStore.java').read_text()
    quic=(ROOT/'android/app/src/main/java/io/mbox/android/QuicLabTransport.java').read_text()
    build=(ROOT/'android/tools/build-core.sh').read_text()
    gradle=(ROOT/'android/app/build.gradle').read_text()
    assert 'dt.put("quic_url",x.quicUrl)' in java
    assert 'dt.put("quic_ca_pem",x.quicCaPem)' in java
    assert 'quicCertFingerprint' in java
    assert 'ProfileStore.quicConfig(context)' in quic
    assert 'AppRuntime.udpLabEnabled(context)' in quic
    assert 'app/src/main/jniLibs/arm64-v8a/libmboxquic.so' in build
    assert "versionName '1.0.0-beta.4.1'" in gradle
    assert 'versionCode 8' in gradle
