from pathlib import Path
import os
import shlex
import subprocess
import zipfile

ROOT = Path(__file__).resolve().parents[1]
GATE = ROOT / 'tools/check-production-apk.sh'


def run_gate(tmp_path, signer_output, manifest_output, signer_exit=0, pinned=True):
    bindir = tmp_path / 'bin'
    bindir.mkdir()
    apk = tmp_path / 'release.apk'
    with zipfile.ZipFile(apk, 'w') as archive:
        archive.writestr('lib/arm64-v8a/libmbox.so', b'fake-core')
        archive.writestr('lib/arm64-v8a/libmboxquic.so', b'fake-quic')
    signer = bindir / 'apksigner'
    # Use printf instead of echo: /bin/sh is bash on Arch but dash on Ubuntu.
    # Their echo implementations disagree about interpreting \n escapes.
    signer.write_text('#!/bin/sh\n' +
                      f"printf '%s\\n' {shlex.quote(signer_output)}\n" +
                      f'exit {signer_exit}\n')
    signer.chmod(0o755)
    aapt = bindir / 'aapt'
    aapt.write_text('#!/bin/sh\n' +
                    f"printf '%s\\n' {shlex.quote(manifest_output)}\n")
    aapt.chmod(0o755)
    env = dict(os.environ, PATH=str(bindir) + ':' + os.environ['PATH'])
    env.pop('MBOX_RELEASE_SIGNER_SHA256', None)
    if pinned:
        env['MBOX_RELEASE_SIGNER_SHA256'] = 'a' * 64
    return subprocess.run(['bash', str(GATE), str(apk)], env=env,
                          text=True, capture_output=True)


SIGNATURE = 'Signer #1 certificate DN: CN=MBox Release, O=Example\nSigner #1 certificate SHA-256 digest: ' + 'a'*64
MANIFEST = "package: name='io.mbox.android' versionCode='8' versionName='1.0.0-beta.4.1'"


def test_accepted_signed_release(tmp_path):
    r = run_gate(tmp_path, SIGNATURE, MANIFEST)
    assert r.returncode == 0, r.stderr
    assert 'PASS' in r.stdout


def test_debug_certificate_rejected(tmp_path):
    r = run_gate(tmp_path, 'Signer #1 certificate DN: CN=Android Debug, O=Android\nSigner #1 certificate SHA-256 digest: ' + 'a' * 64, MANIFEST)
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


def test_release_without_signer_pin_rejected(tmp_path):
    r = run_gate(tmp_path, SIGNATURE, MANIFEST, pinned=False)
    assert r.returncode != 0
    assert 'MBOX_RELEASE_SIGNER_SHA256' in r.stderr


def test_release_wrong_signer_pin_rejected(tmp_path):
    signer = SIGNATURE.replace('a' * 64, 'b' * 64)
    r = run_gate(tmp_path, signer, MANIFEST)
    assert r.returncode != 0
    assert 'does not match' in r.stderr


def test_release_multiple_signers_rejected(tmp_path):
    signer = SIGNATURE + '\nSigner #2 certificate DN: CN=Other'
    r = run_gate(tmp_path, signer, MANIFEST)
    assert r.returncode != 0
    assert 'exactly one signer' in r.stderr


def test_release_wrong_version_rejected(tmp_path):
    assert "versionCode='8'" in MANIFEST
    wrong_manifest = MANIFEST.replace("versionCode='8'", "versionCode='7'")
    assert wrong_manifest != MANIFEST
    r = run_gate(tmp_path, SIGNATURE, wrong_manifest)
    assert r.returncode != 0
    assert 'manifest version' in r.stderr


def test_release_wrong_version_name_rejected(tmp_path):
    assert "versionName='1.0.0-beta.4.1'" in MANIFEST
    wrong_manifest = MANIFEST.replace("versionName='1.0.0-beta.4.1'", "versionName='1.0.0-beta.4'")
    assert wrong_manifest != MANIFEST
    r = run_gate(tmp_path, SIGNATURE, wrong_manifest)
    assert r.returncode != 0
    assert 'manifest version' in r.stderr
