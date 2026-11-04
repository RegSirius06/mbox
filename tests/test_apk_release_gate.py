from pathlib import Path
import os
import subprocess

ROOT = Path(__file__).resolve().parents[1]
GATE = ROOT / 'tools/check-production-apk.sh'


def run_gate(tmp_path, signer_output, manifest_output, signer_exit=0):
    bindir = tmp_path / 'bin'
    bindir.mkdir()
    apk = tmp_path / 'release.apk'
    apk.write_bytes(b'fake-apk-for-script-test')
    signer = bindir / 'apksigner'
    signer.write_text('#!/bin/sh\n' + 'echo ' + repr(signer_output) + '\n' + f'exit {signer_exit}\n')
    signer.chmod(0o755)
    aapt = bindir / 'aapt'
    aapt.write_text('#!/bin/sh\n' + 'echo ' + repr(manifest_output) + '\n')
    aapt.chmod(0o755)
    env = dict(os.environ, PATH=str(bindir) + ':' + os.environ['PATH'])
    return subprocess.run(['bash', str(GATE), str(apk)], env=env,
                          text=True, capture_output=True)


SIGNATURE = 'Signer #1 certificate DN: CN=MBox Release, O=Example'
MANIFEST = "package: name='io.mbox.android' versionCode='5' versionName='1.0.0-beta.2.2'"


def test_accepted_signed_release(tmp_path):
    r = run_gate(tmp_path, SIGNATURE, MANIFEST)
    assert r.returncode == 0, r.stderr
    assert 'PASS' in r.stdout


def test_debug_certificate_rejected(tmp_path):
    r = run_gate(tmp_path, 'Signer #1 certificate DN: CN=Android Debug, O=Android', MANIFEST)
    assert r.returncode != 0
    assert 'debug certificate' in r.stderr


def test_manifest_debuggable_rejected(tmp_path):
    r = run_gate(tmp_path, SIGNATURE, MANIFEST + '\napplication-debuggable')
    assert r.returncode != 0
    assert 'debuggable=true' in r.stderr


def test_unsigned_apk_rejected(tmp_path):
    r = run_gate(tmp_path, SIGNATURE, MANIFEST, signer_exit=1)
    assert r.returncode != 0
    assert 'signature verification failed' in r.stderr


def test_wrong_application_id_rejected(tmp_path):
    r = run_gate(tmp_path, SIGNATURE, "package: name='not.mbox.android'")
    assert r.returncode != 0
    assert 'application ID' in r.stderr
