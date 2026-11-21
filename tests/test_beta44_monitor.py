"""Monitoring unit tests; no external SSH or production access."""
import importlib.util
import json
import os
from pathlib import Path
import sys
import time
import pytest

ROOT=Path(__file__).resolve().parents[1]
ADMIN=ROOT/'tools/mbox-admin'

def load(name,path):
    spec=importlib.util.spec_from_file_location(name,path); mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod);return mod

@pytest.fixture
def backend():
    return load('mon_admin',ADMIN/'mbox_admin.py')

@pytest.fixture
def sampler():
    return load('mon_sampler',ADMIN/'relay_monitor_remote.py')


def row(**extra):
    return dict(elapsed_s=0,cpu_pct=None,rss_kib=100,workers=1,active_slots=0,max_slots=4,incomplete=False,turnover=False,**extra)


def test_cpu_delta(sampler):
    a={(1,1):(100,10),(3,1):(100,1)};b={(1,1):(100+sampler.HZ,10),(3,2):(500,1)}
    assert sampler.cpu_delta(a,b,2)==50
    assert sampler.cpu_delta(None,b,2) is None


def test_row_validation(backend):
    r=row();assert backend.monitor_row(json.dumps(r))==r
    for k,v in [('active_slots',5),('cpu_pct',float('inf')),('workers',True),('rss_kib',-1),('elapsed_s',10**300)]:
        with pytest.raises(ValueError):backend.monitor_row(json.dumps(dict(r,**{k:v})))


def test_sampler_fake_proc(sampler,tmp_path):
    web=tmp_path/'web';web.mkdir();private=tmp_path/'private';private.mkdir();locks=private/'locks';locks.mkdir(mode=0o700)
    (web/'relay.json').write_text(json.dumps({'max_slots':4,'runtime_dir':str(locks)}))
    lock=locks/'slot-000.lock';lock.touch(mode=0o600);st=lock.stat();proc=tmp_path/'proc';proc.mkdir()
    (proc/'locks').write_text('1: FLOCK ADVISORY WRITE 100 %02x:%02x:%s 0 EOF\n'%(os.major(st.st_dev),os.minor(st.st_dev),st.st_ino))
    for pid,exe in [(10,str(web/'relay.fcgi')),(20,str(web/'something-else'))]:
        p=proc/str(pid);p.mkdir();(p/'exe').symlink_to(exe);fields=['0']*22;fields[0]='S';fields[11]='10';fields[12]='5';fields[19]='123';fields[21]='4';(p/'stat').write_text(str(pid)+' (relay.fcgi) '+' '.join(fields))
    data,procs=sampler.snapshot(web,private,proc)
    assert data['workers']==1 and data['active_slots']==1 and data['rss_kib']==round(4*sampler.PAGE_KIB)
    assert procs[(10,123)][0]==15


def test_bounded_worker(backend,monkeypatch):
    msg=json.dumps(row())
    argv=[sys.executable,'-c','import sys;sys.stdin.readline();print('+repr(msg)+',flush=True)']
    monkeypatch.setattr(backend,'monitor_settings',lambda:argv)
    backend.monitor_start({'duration':0,'interval':10})
    for _ in range(100):
        status=backend.monitor_status()
        if status['state']!='running':break
        time.sleep(.02)
    assert status['state']=='finished' and len(status['samples'])==1


def test_duplicate_and_stop(backend,monkeypatch):
    argv=[sys.executable,'-c','import sys,time;sys.stdin.readline();time.sleep(60)']
    monkeypatch.setattr(backend,'monitor_settings',lambda:argv)
    backend.monitor_start({'duration':300,'interval':10})
    with pytest.raises(ValueError):backend.monitor_start({'duration':300,'interval':10})
    backend.monitor_stop()
    for _ in range(150):
        status=backend.monitor_status()
        if status['state']!='running':break
        time.sleep(.03)
    assert status['state']=='stopped'


def test_invalid_duration(backend):
    for d,i in [(301,10),(300,1),(True,10),(300,True)]:
        with pytest.raises(ValueError):backend.monitor_start({'duration':d,'interval':i})


def test_remote_python36_syntax():
    import ast
    ast.parse((ADMIN/'relay_monitor_remote.py').read_text(),feature_version=(3,6))
