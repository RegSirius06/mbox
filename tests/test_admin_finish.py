"""Regression tests: admin deletion and expiry validation (no network)."""
import importlib.util
from pathlib import Path
from unittest.mock import Mock
import pytest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('mbox_admin_finish', ROOT / 'tools/mbox-admin/mbox_admin.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

@pytest.fixture
def admin():
    obj = object.__new__(module.AdminConfig)
    obj.run = Mock(return_value='OK')
    return obj

def test_delete_requires_exact_user_id(admin):
    with pytest.raises(ValueError, match='exact user ID'):
        admin.execute('delete', {'id':'alice'})
    with pytest.raises(ValueError, match='exact user ID'):
        admin.execute('delete', {'id':'alice','confirm_id':'bob'})
    admin.run.assert_not_called()

def test_delete_uses_existing_safe_cli(admin):
    assert admin.execute('delete', {'id':'alice','confirm_id':'alice'}) == 'OK'
    admin.run.assert_called_once_with('user', 'delete', 'alice', '--yes')

def test_delete_rejects_invalid_id(admin):
    with pytest.raises(ValueError, match='Invalid user ID'):
        admin.execute('delete', {'id':'-alice','confirm_id':'-alice'})
    admin.run.assert_not_called()

@pytest.mark.parametrize('days',[-30,-1,0,'30',None,1.2,True,3651])
def test_renew_rejects_invalid_days(admin,days):
    with pytest.raises(ValueError, match='days must be 1..3650'):
        admin.execute('renew', {'id':'alice','days':days})
    admin.run.assert_not_called()

def test_renew_positive_days(admin):
    admin.execute('renew', {'id':'alice','days':30})
    admin.run.assert_called_once_with('user','renew','alice','30')

def test_expiry_clear_uses_existing_cli(admin):
    admin.execute('expiry_clear', {'id':'alice'})
    admin.run.assert_called_once_with('user','expiry','alice','--clear')

def test_ui_has_delete_expiry_and_session_recovery():
    html=(ROOT/'tools/mbox-admin/index.html').read_text()
    assert "['Удалить','delete']" in html
    assert 'd.confirm_id=n' in html
    assert 'Сессия администратора истекла' in html
    assert "['Без срока','expiry_clear']" in html
    assert 'positiveDays(n)' in html
