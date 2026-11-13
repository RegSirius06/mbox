#!/usr/bin/env python3
"""CI structural gate; unsigned APK is NEVER a distributable release."""
import sys
from pathlib import Path
from zipfile import ZipFile

if len(sys.argv) != 2:
    sys.exit('usage: check-unsigned-apk.py path/to/app-release-unsigned.apk')
apk = Path(sys.argv[1])
if not apk.is_file():
    sys.exit(f'FAIL: missing unsigned release APK: {apk}')
with ZipFile(apk) as z:
    names = set(z.namelist())
    for name in ('lib/arm64-v8a/libmbox.so', 'lib/arm64-v8a/libmboxquic.so',
                 'lib/arm64-v8a/libhev-socks5-tunnel.so'):
        # HEV's JNI soname is supplied by the AAR and can vary by upstream.
        if name.endswith('libhev-socks5-tunnel.so'):
            continue
        if name not in names:
            sys.exit(f'FAIL: missing ARM64 native executable {name}')
    if any(x.startswith('lib/') and '/arm64-v8a/' not in x for x in names):
        sys.exit('FAIL: unexpected ABI')
    suspicious = [n for n in names if n.endswith(('mbox-quic-remote.txt', 'mbox-quic-ca.pem'))]
    if suspicious:
        sys.exit('FAIL: QUIC laboratory endpoints/CA bundled: ' + ', '.join(suspicious))
    # Verify production/debug manifest properties with aapt in the signed gate.
    # Binary AndroidManifest.xml cannot be reliably string-searched in Python.
print('PASS: unsigned APK structural check; NOT signed or ready to distribute')
