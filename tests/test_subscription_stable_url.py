"""Idempotent persistent subscriptions. Standalone fixtures; no VPS or network."""
import hashlib
import importlib.util
import os
import sqlite3
from pathlib import Path
from concurrent.futures import ThreadPoolExecutor
import pytest

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('mbox_sub_stable',ROOT/'tools/mbox-subscription/mbox_subscription.py')
mod=importlib.util.module_from_spec(spec);spec.loader.exec_module(mod)

@pytest.fixture
def sandbox(tmp_path,monkeypatch):
    etc=tmp_path/'etc';etc.mkdir()
    state=tmp_path/'state';state.mkdir()
    (etc/'mbox.conf').write_text('[server]\npublic_url = https://relay.example/mbox\n[quic]\nserver_name = vps.example\n[subscription]\npublic_url = https://vps.example/api/v2\n')
    con=sqlite3.connect(state/'users.db')
    con.execute('CREATE TABLE users (id TEXT PRIMARY KEY, enabled INTEGER, expires_at TEXT, max_sessions INTEGER)')
    con.execute('INSERT INTO users VALUES(?,?,?,?)', ('alice',1,None,2))
    con.commit();con.close()
    monkeypatch.setattr(mod,'DB',state/'users.db')
    monkeypatch.setattr(mod,'CONF',etc/'mbox.conf')
    monkeypatch.setattr(mod,'LINK_KEY',etc/'subscription-link.key')
    return state/'users.db',etc

def test_canonical_stable_across_clicks_and_restarts(sandbox):
    database,etc=sandbox
    a=mod.get_or_create_subscription('alice')
    assert a.startswith('https://vps.example/api/v2/')
    assert mod.get_or_create_subscription('alice')==a
    assert mod.LINK_KEY.stat().st_mode & 0o777 == 0o600
    with sqlite3.connect(database) as con:
        assert con.execute('SELECT COUNT(*) FROM subscription_tokens').fetchone()[0] == 1
        assert con.execute('SELECT COUNT(*) FROM subscription_primary').fetchone()[0] == 1
        stored=con.execute('SELECT digest FROM subscription_tokens').fetchone()[0]
    assert stored==hashlib.sha256(a.rsplit('/',1)[-1].encode()).hexdigest()
    assert a.rsplit('/',1)[-1] not in database.read_bytes().decode('latin1')
    # Change only Caddy route + subscription.public_url; stored token unchanged.
    config=etc/'mbox.conf'
    config.write_text(config.read_text().replace('/api/v2','/somewhere/else'))
    b=mod.get_or_create_subscription('alice')
    assert b.startswith('https://vps.example/somewhere/else/')
    assert b.rsplit('/',1)[-1] == a.rsplit('/',1)[-1]

def test_legacy_tokens_stay_valid_and_counted(sandbox):
    database,_=sandbox
    older='A'*43
    with mod.db() as con:
        mod.schema(con)
        con.execute('INSERT INTO subscription_tokens VALUES(?,?,?,NULL)',(hashlib.sha256(older.encode()).hexdigest(),'alice',12))
    mod.get_or_create_subscription('alice')
    with mod.db() as con:
        assert con.execute('SELECT COUNT(*) FROM subscription_tokens WHERE revoked_at IS NULL').fetchone()[0] == 2
        assert con.execute('SELECT 1 FROM subscription_tokens WHERE digest=?',(hashlib.sha256(older.encode()).hexdigest(),)).fetchone()

def test_revocation_and_recreation(sandbox):
    database,_=sandbox
    old=mod.get_or_create_subscription('alice')
    assert mod.revoke_subscriptions('alice')==1
    new=mod.get_or_create_subscription('alice')
    assert old!=new
    with mod.db() as con:
        assert con.execute('SELECT count(*) FROM subscription_primary').fetchone()[0]==1
        active=con.execute('SELECT digest FROM subscription_tokens WHERE revoked_at IS NULL').fetchall()
    assert len(active)==1 and active[0][0]==hashlib.sha256(new.rsplit('/',1)[-1].encode()).hexdigest()

def test_deleting_then_recreating_id_never_revives_old_link(sandbox):
    database,_=sandbox
    old=mod.get_or_create_subscription('alice')
    with mod.db() as con:
        con.execute('DELETE FROM users WHERE id=?',('alice',))
        con.execute('INSERT INTO users VALUES(?,?,?,?)',('alice',1,None,2))
    with mod.db() as con:
        assert con.execute('SELECT count(*) FROM subscription_tokens WHERE revoked_at IS NULL').fetchone()[0]==0
        assert con.execute('SELECT count(*) FROM subscription_primary').fetchone()[0]==0
    new=mod.get_or_create_subscription('alice')
    assert old!=new

def test_restore_without_master_key_fails_closed(sandbox):
    database,_=sandbox
    original=mod.get_or_create_subscription('alice')
    mod.LINK_KEY.unlink()
    with pytest.raises(ValueError,match='restore'):
        mod.get_or_create_subscription('alice')
    assert not mod.LINK_KEY.exists()
    with mod.db() as con:
        assert con.execute('SELECT COUNT(*) FROM subscription_tokens').fetchone()[0]==1

def test_concurrent_get_creates_one_token(sandbox):
    with ThreadPoolExecutor(max_workers=8) as pool:
        urls=list(pool.map(lambda _:mod.get_or_create_subscription('alice'),range(12)))
    assert len(set(urls))==1
    with mod.db() as con:
        assert con.execute('SELECT COUNT(*) FROM subscription_tokens').fetchone()[0]==1

def test_disabled_never_issues(sandbox):
    with mod.db() as con:
        con.execute('UPDATE users SET enabled=0')
    with pytest.raises(ValueError,match='disabled'):
        mod.get_or_create_subscription('alice')
    assert not mod.LINK_KEY.exists()

def test_cli_alias_now_idempotent(sandbox, capsys):
    from argparse import Namespace
    mod.command(Namespace(operation='issue',id='alice'))
    a=capsys.readouterr().out.strip()
    mod.command(Namespace(operation='issue',id='alice'))
    assert capsys.readouterr().out.strip()==a
