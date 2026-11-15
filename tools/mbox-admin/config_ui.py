#!/usr/bin/env python3
"""Versioned/validated browser editing of mbox.conf and the single public subscription route.

No secret values are returned to the browser. Source of truth: mbox.conf.
Only config that the CLI can validate is committed. No shell command execution.
"""
import configparser
import hashlib
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import threading
from datetime import datetime, timezone
from urllib.parse import urlsplit

CONFIG = Path(os.environ.get('MBOX_ETC', '/etc/mbox')) / 'mbox.conf'
CADDY = Path(os.environ.get('MBOX_CADDYFILE', '/etc/caddy/Caddyfile'))
BACKUPS = Path(os.environ.get('MBOX_CONFIG_BACKUPS', '/root/mbox-config-backups'))
LOCK = threading.Lock()
KEY = re.compile(r'[A-Za-z][A-Za-z0-9_-]{0,63}\Z')
PATH = re.compile(r'(?:/[A-Za-z0-9_-]{1,32}){1,4}\Z')
MASK = re.compile(r'password|secret|token|private_key|credential|psk', re.I)
SUB_PROXY = '127.0.0.1:18450'


def checked_file(path):
    if path.is_symlink() or not path.is_file() or path.stat().st_size > 262144:
        raise ValueError('Unsafe or missing configuration file')
    return path.read_bytes()


def parse(content):
    cp = configparser.ConfigParser(interpolation=None)
    cp.read_string(content.decode('utf-8'))
    return cp


def original_url(cp):
    value = cp.get('subscription', 'public_url', fallback='').strip()
    if not value:
        host = cp.get('quic','server_name',fallback='').strip()
        if not host or not re.fullmatch(r'[A-Za-z0-9.-]{1,253}', host):
            raise ValueError('Cannot determine HTTPS subscription host from quic.server_name')
        return 'https://'+host+'/mbox/sub'
    return value


def check_public_url(value, cp):
    if not isinstance(value, str) or len(value) > 512:
        raise ValueError('Invalid subscription URL')
    u = urlsplit(value)
    host = cp.get('quic','server_name',fallback='').strip()
    if u.scheme != 'https' or u.hostname != host or u.port not in (None,443) or u.username or u.password or u.query or u.fragment or not PATH.fullmatch(u.path):
        raise ValueError('Subscription URL must use the current Caddy HTTPS domain and a safe path, e.g. /api/v2')
    return u.path


def snapshot():
    content=checked_file(CONFIG)
    cp=parse(content)
    sections={}
    for section in cp.sections():
        rows=[]
        for key,value in cp.items(section, raw=True):
            rows.append({'key':key,'value':'' if MASK.search(key) else value,
                         'secret':bool(MASK.search(key))})
        sections[section]=rows
    if 'subscription' not in sections:
        sections['subscription']=[]
    if not any(row['key']=='public_url' for row in sections['subscription']):
        sections['subscription'].append({'key':'public_url','value':original_url(cp),'secret':False})
    return {'revision':hashlib.sha256(content).hexdigest(), 'sections':sections,
            'subscription_url':original_url(cp)}


def modify_ini(text, changes):
    """Edit only named INI assignments; keep unrelated comments and original formatting."""
    lines=text.splitlines(keepends=True)
    sections=[]
    for i,line in enumerate(lines):
        m=re.match(r'^\s*\[([^\]]+)\]\s*(?:[;#].*)?$',line.strip())
        if m: sections.append((i,m.group(1).strip().lower()))
    for section,key,value in sorted(changes, key=lambda x:(x[0],x[1])):
        found=[i for i,name in sections if name==section]
        if len(found)>1: raise ValueError('Repeated INI section: '+section)
        if not found:
            if (section,key)!=('subscription','public_url'):
                raise ValueError('New config keys not supported: '+section+'.'+key)
            if lines and not lines[-1].endswith('\n'):lines[-1]+='\n'
            lines+=['\n[subscription]\n','public_url = '+value+'\n']
        else:
            start=found[0]
            end=next((i for i,name in sections if i>start),len(lines))
            indexes=[i for i in range(start+1,end) if re.match(r'^\s*'+re.escape(key)+r'\s*[:=]',lines[i],re.I)]
            if len(indexes)>1:raise ValueError('Repeated config key: '+section+'.'+key)
            if not indexes and (section,key)!=('subscription','public_url'):
                raise ValueError('Unknown config key: '+section+'.'+key)
            if indexes:lines[indexes[0]]=key+' = '+value+'\n'
            else:lines.insert(end,key+' = '+value+'\n')
        sections=[]
        for i,line in enumerate(lines):
            m=re.match(r'^\s*\[([^\]]+)\]\s*(?:[;#].*)?$',line.strip())
            if m:sections.append((i,m.group(1).strip().lower()))
    return ''.join(lines)


def switch_route(caddy_text,old_path,new_path):
    if old_path == new_path:
        return caddy_text
    lines=caddy_text.splitlines(keepends=True)
    candidate=[]
    for i,line in enumerate(lines):
        m=re.fullmatch(r'\s*handle\s+(/[^\s{}*]+)/\*\s*\{\s*',line.strip())
        if not m or m.group(1).rstrip('/')!=old_path:continue
        depth=1
        for j in range(i+1,len(lines)):
            # Refuse ambiguous nested/quoted Caddy configurations.
            if '"' in lines[j] or "'" in lines[j]:
                raise ValueError('Complex Caddy route: edit using dedicated deployment tool')
            depth+=lines[j].count('{')-lines[j].count('}')
            if depth==0:
                block=''.join(lines[i:j+1])
                if ('reverse_proxy '+SUB_PROXY) in block:
                    candidate.append((i,j,block))
                break
        else:raise ValueError('Unbalanced Caddyfile')
    if len(candidate)!=1:
        raise ValueError('Expected exactly one owned subscription Caddy route at '+old_path)
    if any('handle '+new_path+'/*' in line for line in lines):
        raise ValueError('Target Caddy route already exists')
    start,end,block=candidate[0]
    rewritten=block.replace('handle '+old_path+'/*','handle '+new_path+'/*',1)
    # Internal service accepts /mbox/sub/<token>. New public path has exactly
    # one rewrite; the OLD public path is no longer served by Caddy.
    bl=rewritten.splitlines(keepends=True)
    bl=[line for line in bl if not re.match(r'^\s*uri replace ',line)]
    if new_path!='/mbox/sub':
        bl.insert(1,'        uri replace '+new_path+'/ /mbox/sub/ 1\n')
    lines[start:end+1]=bl
    return ''.join(lines)


def atomic(path,data):
    st=path.stat()
    fd,name=tempfile.mkstemp(prefix='.'+path.name+'.mbox-',dir=str(path.parent))
    try:
        with os.fdopen(fd,'wb') as f:
            f.write(data); f.flush();os.fsync(f.fileno())
        os.chmod(name,st.st_mode & 0o777)
        os.chown(name,st.st_uid,st.st_gid)
        os.replace(name,path)
    finally:
        if os.path.exists(name):os.unlink(name)


def call(args):
    result=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=25)
    if result.returncode:
        # Trusted fixed labels; never put subprocess stderr/stdout or secrets in UI.
        step = {
            ('/usr/local/bin/mboxctl','config','validate'): 'MBox config validation',
            ('/usr/local/bin/mbox-subscription','check'): 'subscription config validation',
            ('caddy','validate'): 'Caddy configuration validation',
            ('systemctl','reload'): 'Caddy reload',
            ('/usr/local/bin/mboxctl','sync'): 'MBox runtime sync',
            ('systemctl','is-active'): 'MBox origin status',
        }.get(tuple(args[:2]) if args[:1]!=['/usr/local/bin/mboxctl'] else tuple(args[:3]), 'server operation')
        raise RuntimeError('Cannot apply changes: ' + step + ' failed; previous config restored')


def apply(revision, changes, confirmation):
    if confirmation!='APPLY':raise ValueError('Type APPLY to confirm production changes')
    if not isinstance(revision,str) or not re.fullmatch(r'[0-9a-f]{64}',revision):
        raise ValueError('Invalid revision')
    if not isinstance(changes,list) or not 1<=len(changes)<=96:
        raise ValueError('Expected 1..96 changed fields')
    with LOCK:
        old=checked_file(CONFIG)
        if hashlib.sha256(old).hexdigest()!=revision:
            raise ValueError('mbox.conf changed; refresh configuration before saving')
        cp=parse(old)
        edits=[]; keys=set()
        for row in changes:
            if not isinstance(row,dict):raise ValueError('Invalid field')
            sec,key,val=row.get('section'),row.get('key'),row.get('value')
            if not isinstance(sec,str) or not KEY.fullmatch(sec) or not isinstance(key,str) or not KEY.fullmatch(key):
                raise ValueError('Invalid section/key')
            if not isinstance(val,str) or len(val)>8192 or any(c in val for c in '\r\n\x00'):
                raise ValueError('Invalid field value')
            if (sec,key) in keys:raise ValueError('Duplicate field')
            keys.add((sec,key))
            if sec!='subscription' or key!='public_url':
                if not cp.has_option(sec,key):raise ValueError('Unknown field: '+sec+'.'+key)
                if MASK.search(key) and not val:continue # Empty masked input retains old secret
                if cp.get(sec,key,raw=True)==val:continue
            edits.append((sec,key,val))
        if not edits:return {'ok':True,'message':'No changes'}
        proposed=modify_ini(old.decode('utf-8'),edits).encode('utf-8')
        target=parse(proposed)
        prev_url=original_url(cp);next_url=original_url(target)
        new_path=check_public_url(next_url,target)
        old_path=check_public_url(prev_url,cp)
        if old_path!=new_path and target.get('quic','server_name',fallback='')!=cp.get('quic','server_name',fallback=''):
            raise ValueError('Change TLS domain separately from subscription path')
        caddy_old=caddy_new=None
        if old_path!=new_path:
            caddy_old=checked_file(CADDY)
            caddy_new=switch_route(caddy_old.decode('utf-8'),old_path,new_path).encode('utf-8')
        # No side effects before the backup exists.
        BACKUPS.mkdir(parents=True,exist_ok=True,mode=0o700)
        if BACKUPS.is_symlink() or (BACKUPS.stat().st_mode & 0o077):
            raise ValueError('Unsafe backup directory permissions')
        stamp=datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'-'+str(os.getpid())
        location=BACKUPS/stamp
        location.mkdir(mode=0o700)
        (location/'mbox.conf').write_bytes(old)
        os.chmod(location/'mbox.conf',0o600)
        if caddy_old is not None:
            (location/'Caddyfile').write_bytes(caddy_old)
            os.chmod(location/'Caddyfile',0o600)
        # Materialize config, validate, stage Caddy, then reload once.
        touched_origin=any(s not in ('quic','subscription') for s,k,v in edits)
        try:
            atomic(CONFIG,proposed)
            call(['/usr/local/bin/mboxctl','config','validate'])
            if any(s == 'subscription' for s,k,v in edits):
                call(['/usr/local/bin/mbox-subscription','check'])
            if caddy_new is not None:
                atomic(CADDY,caddy_new)
                call(['caddy','validate','--config',str(CADDY)])
                call(['systemctl','reload','caddy.service'])
            if touched_origin:
                call(['/usr/local/bin/mboxctl','sync','--restart'])
                call(['systemctl','is-active','--quiet','mbox-server.service'])
        except Exception:
            atomic(CONFIG,old)
            if caddy_old is not None:
                atomic(CADDY,caddy_old)
                subprocess.run(['systemctl','reload','caddy.service'],timeout=25,check=False)
            if touched_origin:
                subprocess.run(['/usr/local/bin/mboxctl','sync','--restart'],timeout=25,check=False)
            raise
        return {'ok':True,'backup':str(location),'restart_origin':touched_origin,
                'url_changed':old_path!=new_path,
                'message':'Applied; previously issued subscription URLs must be redistributed' if old_path!=new_path else 'Applied'}
