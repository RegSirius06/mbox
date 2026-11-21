#!/usr/bin/env python3
"""Read-only beta.4.4 current documentation and release identity gate.

Dated earlier-beta docs are historical evidence and are intentionally excluded
from current-version assertions. Fail closed on missing links in active docs.
"""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
VERSION = '1.0.0-beta.4.4'
ACTIVE = [
    'README.md', 'README.ru.md', 'CHANGELOG.md', 'CHANGELOG.ru.md',
    'RELEASE_NOTES.md', 'RELEASE_NOTES.ru.md',
    'RELEASE_CHECKLIST.md', 'RELEASE_CHECKLIST.ru.md',
    'docs/README.md', 'docs/ru/README.md',
    'docs/ARCHITECTURE.md', 'docs/ru/ARCHITECTURE.md',
    'docs/STATUS.md', 'docs/ru/STATUS.md',
    'docs/BETA44_DATAGRAM_EXPERIMENT.md', 'docs/ru/BETA44_DATAGRAM_EXPERIMENT.md',
    'docs/GITHUB_RELEASE_BODY.md', 'docs/ru/GITHUB_RELEASE_BODY.md',
    'docs/GETTING_STARTED.md', 'docs/ru/GETTING_STARTED.md',
    'docs/SERVER_QUICKSTART.md', 'docs/ru/SERVER_QUICKSTART.md',
    'android/README.md', 'android/README.ru.md',
]
REQUIRED = [
    ('README.md', 'DATAGRAM'), ('README.ru.md', 'DATAGRAM'),
    ('RELEASE_NOTES.md', 'REG.RU'), ('RELEASE_NOTES.ru.md', 'REG.RU'),
    ('docs/ARCHITECTURE.md', 'MBD2'), ('docs/ru/ARCHITECTURE.md', 'MBD2'),
    ('docs/BETA44_DATAGRAM_EXPERIMENT.md', 'No periodic'),
    ('docs/ru/BETA44_DATAGRAM_EXPERIMENT.md', 'PING'),
    ('docs/GITHUB_RELEASE_BODY.md', 'PENDING_SIGNED_APK_SHA256'),
    ('docs/ru/GITHUB_RELEASE_BODY.md', 'PENDING_SIGNED_APK_SHA256'),
]

def fail(message):
    print('FAIL:', message, file=sys.stderr)
    return False

def check():
    ok = True
    if (ROOT/'VERSION').read_text().strip() != VERSION:
        ok = fail('VERSION is not beta.4.4')
    code=(ROOT/'android/app/build.gradle').read_text()
    for part in ("versionCode 11", "versionName '1.0.0-beta.4.4'", "applicationId 'io.mbox.android'"):
        if part not in code: ok = fail('Gradle version or applicationId: '+part)
    for rel, marker in (
        ('tools/mboxctl/mboxctl.py', 'mboxctl 1.0.0-beta.4.4'),
        ('tools/build-release-apk.sh', VERSION),
        ('tools/check-production-apk.sh', "versionCode='11' versionName='1.0.0-beta.4.4'"),
    ):
        if marker not in (ROOT/rel).read_text(): ok=fail('release source mismatch: '+rel)
    for rel in ACTIVE:
        p=ROOT/rel
        if not p.is_file(): ok=fail('missing active doc: '+rel); continue
        s=p.read_text(encoding='utf-8')
        if VERSION not in s: ok=fail('outdated current release doc: '+rel)
        # No attempt to treat external links, mailto, anchors, or templated URLs as files.
        for target in re.findall(r'(?<!!).*?\[[^\]\n]+\]\(([^)]+)\)',s):
            path=target.split('#',1)[0].split('?',1)[0]
            if not path or path.startswith(('https://','http://','mailto:','/')): continue
            if not (p.parent/path).exists(): ok=fail('dead local link: '+rel+' -> '+path)
    for rel, marker in REQUIRED:
        if marker not in (ROOT/rel).read_text(): ok=fail('missing beta.4.4 feature/risk marker: '+rel+': '+marker)
    if ok: print(f'PASS: beta.4.4 version, signer gate and {len(ACTIVE)} current EN/RU docs checked')
    return 0 if ok else 1

if __name__=='__main__': sys.exit(check())
