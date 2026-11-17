#!/usr/bin/env python3
"""Read-only MBox beta.4.3 active docs and source consistency check.

Does not consider historical release-specific docs to be current instructions.
"""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
VERSION = '1.0.0-beta.4.3'
DOCS = [
    'README.md', 'README.ru.md',
    'docs/README.md', 'docs/ru/README.md',
    'docs/GETTING_STARTED.md', 'docs/ru/GETTING_STARTED.md',
    'docs/SERVER_QUICKSTART.md', 'docs/ru/SERVER_QUICKSTART.md',
    'docs/RELEASE_ENGINEERING_BETA43.md', 'docs/ru/RELEASE_ENGINEERING_BETA43.md',
    'android/README.md', 'android/README.ru.md',
    'docs/ANDROID_RELEASE_ENGINEERING.md', 'docs/ru/ANDROID_RELEASE_ENGINEERING.md',
    'RELEASE_CHECKLIST.md', 'RELEASE_CHECKLIST.ru.md',
    'RELEASE_NOTES.md', 'RELEASE_NOTES.ru.md',
    'docs/STATUS.md', 'docs/ru/STATUS.md',
    'docs/BETA4_3_STATUS.md', 'docs/ru/BETA4_3_STATUS.md',
    'docs/GITHUB_RELEASE_BODY.md', 'docs/ru/GITHUB_RELEASE_BODY.md',
]


def fail(message):
    print('FAIL:', message, file=sys.stderr)
    return False


def check():
    ok = True
    if (ROOT / 'VERSION').read_text().strip() != VERSION:
        ok = fail('VERSION mismatch')
    gradle = (ROOT / 'android/app/build.gradle').read_text()
    for part in ('versionCode 10', "versionName '1.0.0-beta.4.3'", "applicationId 'io.mbox.android'"):
        if part not in gradle:
            ok = fail('Gradle missing ' + part)
    for rel, needed in (
        ('tools/mboxctl/mboxctl.py', 'mboxctl 1.0.0-beta.4.3'),
        ('tools/build-release-apk.sh', VERSION),
        ('tools/check-production-apk.sh', "versionCode='10' versionName='1.0.0-beta.4.3'"),
        ('tools/mbox-admin/index.html', 'beta.4.3'),
    ):
        if not (ROOT / rel).is_file() or needed not in (ROOT / rel).read_text():
            ok = fail(f'{rel}: expected source version marker not found')
    for rel in DOCS:
        path = ROOT / rel
        if not path.is_file():
            ok = fail(f'missing active document: {rel}')
            continue
        content = path.read_text('utf-8')
        if VERSION not in content:
            ok = fail(f'{rel}: missing current beta.4.3 version')
        if rel.startswith(('README', 'RELEASE_', 'docs/', 'android/')):
            for target in re.findall(r'(?<!!)\[[^\]\n]+\]\(([^)]+)\)', content):
                target = target.split('#', 1)[0].split('?', 1)[0]
                if not target or target.startswith(('https://', 'http://', 'mailto:')):
                    continue
                if not (path.parent / target).exists():
                    ok = fail(f'{rel}: missing local link: {target}')
    for suffix in ('', '.ru'):
        notes = (ROOT / f'RELEASE_NOTES{suffix}.md').read_text()
        if not any(x in notes for x in ('prerelease', 'предварительн', 'beta, not RC1')):
            ok = fail(f'RELEASE_NOTES{suffix}.md: prerelease classification missing')
    if ok:
        print(f'PASS: beta.4.3 source, APK manifest checks and {len(DOCS)} active docs are aligned')
    return 0 if ok else 1


if __name__ == '__main__':
    sys.exit(check())
