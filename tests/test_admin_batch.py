"""MBox Admin batch add: bounded, safe, existing CLI-only behavior."""
import importlib.util
from pathlib import Path
from unittest.mock import Mock
import pytest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('mbox_admin_batch', ROOT/'tools/mbox-admin/mbox_admin.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

@pytest.fixture
def admin():
    obj = object.__new__(module.AdminConfig)
    obj.run = Mock(return_value='Created 3 users')
    return obj

def test_batch_creates_individual_psk_accounts_via_cli(admin):
    admin.execute('add_batch', {'prefix':'staff-','count':3,'max_sessions':2,'days':30})
    admin.run.assert_called_once_with('user','add','--count','3','--prefix','staff-','--width','3','--max-sessions','2','--days','30')

def test_batch_defaults(admin):
    admin.execute('add_batch', {'prefix':'user-','count':2})
    admin.run.assert_called_once_with('user','add','--count','2','--prefix','user-','--width','3')

@pytest.mark.parametrize('count',[None,0,1,101,-5,True,'3',3.5])
def test_batch_invalid_count_does_not_run(admin,count):
    with pytest.raises(ValueError): admin.execute('add_batch', {'prefix':'user-','count':count})
    admin.run.assert_not_called()

@pytest.mark.parametrize('prefix',['','-bad','not good','x'*61,None,'../'])
def test_batch_invalid_prefix_does_not_run(admin,prefix):
    with pytest.raises(ValueError): admin.execute('add_batch', {'prefix':prefix,'count':3})
    admin.run.assert_not_called()

@pytest.mark.parametrize('field,value', [('days',-1),('days',True),('days',3651),('max_sessions',0),('max_sessions',-1),('max_sessions','2'),('max_sessions',True)])
def test_batch_invalid_optional_limits_does_not_run(admin,field,value):
    with pytest.raises(ValueError): admin.execute('add_batch', {'prefix':'user-','count':3,field:value})
    admin.run.assert_not_called()

def test_ui_wires_batch_to_api():
    h=(ROOT/'tools/mbox-admin/index.html').read_text()
    assert 'id="newbatch"' in h
    assert "action:'add_batch'" in h
    assert "'add_batch','enable'" in (ROOT/'tools/mbox-admin/mbox_admin.py').read_text()
