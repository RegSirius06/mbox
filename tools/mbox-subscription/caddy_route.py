#!/usr/bin/env python3
"""Install Caddyfile subscription route conservatively with validation and rollback.

Only changes an exact single-host site block, not global Caddy config. Stdlib.
"""
import argparse
import configparser
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
from datetime import datetime, timezone

CONF = Path('/etc/mbox/mbox.conf')
ROUTE = '    handle /mbox/sub/* {\n        log_skip\n        reverse_proxy 127.0.0.1:18450\n    }\n'


def domain_name():
    cfg = configparser.ConfigParser(interpolation=None)
    if not cfg.read(CONF):
        raise ValueError('missing /etc/mbox/mbox.conf')
    name = cfg.get('quic', 'server_name', fallback='').strip()
    if not name or not re.fullmatch(r'[A-Za-z0-9.-]{1,253}', name):
        raise ValueError('set [quic] server_name in mbox.conf before installing Caddy route')
    return name


def edit_caddy(text, domain):
    if 'reverse_proxy 127.0.0.1:18450' in text or '/mbox/sub/*' in text:
        raise ValueError('a subscription route already exists; inspect manually')
    lines = text.splitlines(keepends=True)
    starts = [i for i, line in enumerate(lines)
              if re.fullmatch(r'\s*(?:https://)?' + re.escape(domain) + r'(?::443)?\s*\{\s*(?:#.*)?\n?', line)]
    if len(starts) != 1:
        raise ValueError('expected exactly one standalone Caddyfile site block for '+domain+'; refusing complex or JSON config')
    start = starts[0]
    # Conservative syntax detection; refuse quoted braces and inline nested site blocks.
    depth = 1
    closed = False
    for line in lines[start+1:]:
        if '"' in line or "'" in line:
            raise ValueError('quoted Caddyfile block: manual integration required')
        stripped = line.split('#', 1)[0]
        depth += stripped.count('{') - stripped.count('}')
        if depth < 0:
            raise ValueError('unbalanced Caddyfile')
        if depth == 0:
            closed = True
            break
    if not closed:
        raise ValueError('unbalanced site block')
    lines.insert(start+1, ROUTE)
    return ''.join(lines)


def main():
    p = argparse.ArgumentParser()
    group = p.add_mutually_exclusive_group(required=True)
    group.add_argument('--check', action='store_true')
    group.add_argument('--apply', action='store_true')
    p.add_argument('--file', type=Path, default=Path('/etc/caddy/Caddyfile'))
    a = p.parse_args()
    f = a.file
    if f.is_symlink() or not f.is_file() or f.stat().st_size > 262144:
        p.error('expected real Caddyfile <=256 KiB')
    domain = domain_name()
    original = f.read_text()
    proposed = edit_caddy(original, domain)
    if a.check:
        print('CADDY PREFLIGHT OK: can add /mbox/sub/* to', domain)
        return 0
    if os.geteuid() != 0:
        p.error('apply requires root')
    if subprocess.run(['systemctl','is-active','--quiet','mbox-subscription.service']).returncode:
        p.error('subscription service inactive')
    backup = f.parent / (f.name+'.mbox-subs.'+datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'.bak')
    shutil.copy2(f, backup)
    tmp = f.with_name('.'+f.name+'.mbox-subs.tmp')
    try:
        fd = os.open(tmp, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, 0o600)
        with os.fdopen(fd, 'w') as out:
            out.write(proposed)
            out.flush(); os.fsync(out.fileno())
        os.chmod(tmp, f.stat().st_mode & 0o777)
        os.chown(tmp, f.stat().st_uid, f.stat().st_gid)
        os.replace(tmp, f)
        if subprocess.run(['caddy','validate','--config',str(f)]).returncode:
            raise RuntimeError('Caddy validation failed')
        if subprocess.run(['systemctl','reload','caddy.service']).returncode:
            raise RuntimeError('Caddy reload failed')
        print('CADDY SUBSCRIPTIONS LIVE: https://'+domain+'/mbox/sub/<secret>')
        print('BACKUP:', backup)
        return 0
    except Exception:
        shutil.copy2(backup, f)
        subprocess.run(['systemctl','reload','caddy.service'], check=False)
        raise
    finally:
        tmp.unlink(missing_ok=True)

if __name__ == '__main__':
    try:
        sys.exit(main())
    except (OSError, ValueError, RuntimeError) as e:
        print('ERROR:', e, file=sys.stderr)
        sys.exit(1)
