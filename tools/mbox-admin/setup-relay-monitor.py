#!/usr/bin/env python3
"""Run on VPS as root. Provision restricted monitoring key, not deployment access."""
import base64
import json
import os
import re
import shlex
import subprocess
from pathlib import Path

D=Path('/etc/mbox-admin/relay-monitor')
PROFILE=Path('/etc/mbox/relay-deploy.json')


def main():
    if os.geteuid()!=0: raise SystemExit('Run as root')
    os.umask(0o077)
    cfg=json.loads(PROFILE.read_text())
    host,web,private=(cfg[k] for k in ('ssh','remote_dir','remote_private_dir'))
    if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9._-]*@[A-Za-z0-9][A-Za-z0-9.-]*',host): raise SystemExit('Use explicit user@host in relay profile')
    if not all(re.fullmatch(r'/[A-Za-z0-9_./-]+',x) and os.path.normpath(x)==x for x in (web,private)):raise SystemExit('Invalid paths')
    server=host.split('@',1)[1]
    # Reuse only a previously accepted host key. Never automatically trust ssh-keyscan.
    result=subprocess.run(['ssh-keygen','-F',server,'-f','/root/.ssh/known_hosts'],text=True,capture_output=True,check=True)
    known='\n'.join(x for x in result.stdout.splitlines() if x and not x.startswith('#'))+'\n'
    if not known.strip():raise SystemExit('No trusted host key; check SSH manually first')
    D.mkdir(mode=0o700,parents=True,exist_ok=True)
    if D.is_symlink() or D.stat().st_uid!=0:raise SystemExit('Unsafe key directory')
    D.chmod(0o700)
    key=D/'id_ed25519'
    if not key.exists():subprocess.run(['ssh-keygen','-q','-t','ed25519','-N','','-C','mbox-relay-monitor','-f',str(key)],check=True)
    key.chmod(0o600)
    pub=key.with_suffix('.pub').read_text().strip().split()[:2]
    raw=Path(__file__).with_name('relay_monitor_remote.py').read_bytes()
    # Install in private home, outside web root. Preserve all other authorized keys.
    payload='\n'.join([
      'import base64, os, shlex, stat', 'from pathlib import Path', 'os.umask(0o077)',
      'home=Path.home(); d=home/\'.mbox-monitor\'; ssh=home/\'.ssh\'',
      'for p in (d,ssh):\n    p.mkdir(mode=0o700,exist_ok=True)\n    if p.is_symlink() or p.stat().st_uid!=os.getuid() or p.stat().st_mode & 0o022: raise SystemExit("unsafe directory")',
      'script=d/\'sampler.py\'; auth=ssh/\'authorized_keys\'',
      'for p in (script,auth):\n    if p.is_symlink() or (p.exists() and (not p.is_file() or p.stat().st_uid!=os.getuid())): raise SystemExit("unsafe file")',
      'script.write_bytes(base64.b64decode('+repr(base64.b64encode(raw).decode())+')); script.chmod(0o600)',
      'command="python3 "+shlex.quote(str(script))+" "+shlex.quote('+repr(web)+')+" "+shlex.quote('+repr(private)+')',
      'if any(c in command for c in (chr(34),chr(10),chr(92))): raise SystemExit("unsafe command")',
      'pub='+repr(' '.join(pub)),
      'line=\'command="\'+command+\'",no-port-forwarding,no-agent-forwarding,no-X11-forwarding,no-pty \'+pub+\' mbox-relay-monitor\'',
      'old=auth.read_text().splitlines() if auth.exists() else []',
      'if any(pub.split()[1] in x and x!=line for x in old): raise SystemExit("key already exists with different restrictions")',
      'if line not in old:\n    with auth.open("a") as f: f.write("\\n"+line+"\\n")',
      'auth.chmod(0o600); print("MONITOR_KEY_INSTALLED")',
    ])
    (D/'known_hosts').write_text(known); (D/'known_hosts').chmod(0o600)
    # Password entered via SSH terminal, never saved or passed to subprocess argv.
    subprocess.run(['ssh','-F','/dev/null','-T','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(D/'known_hosts'),'-o','GlobalKnownHostsFile=/dev/null',host,'python3 -'],input=payload.encode(),check=True)
    argv=['ssh','-F','/dev/null','-T','-o','BatchMode=yes','-o','IdentitiesOnly=yes','-o','StrictHostKeyChecking=yes','-o','UserKnownHostsFile='+str(D/'known_hosts'),'-o','GlobalKnownHostsFile=/dev/null','-i',str(key),host]
    res=subprocess.run(argv+['mbox-monitor'],input=b'{"duration":0,"interval":10}\n',capture_output=True,timeout=25,check=True)
    row=json.loads(res.stdout); assert 'workers' in row and 'max_slots' in row
    bad=subprocess.run(argv+['true'],input=b'',capture_output=True,timeout=20)
    if bad.returncode==0:raise SystemExit('Forced command NOT enforced. Remove monitoring key from remote authorized_keys before proceeding.')
    (D/'config.json').write_text(json.dumps({'host':host})+'\n'); (D/'config.json').chmod(0o600)
    print('MONITOR_READY',res.stdout.decode().strip())

if __name__=='__main__': main()
