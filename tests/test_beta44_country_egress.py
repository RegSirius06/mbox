"""Country exits: authenticated Noise choice, one ingress, stable subscription URL."""
import base64
import hashlib
import importlib.util
import json
import sqlite3
from pathlib import Path
from types import SimpleNamespace
import pytest

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / 'tools/mbox-subscription/mbox_subscription.py'
spec = importlib.util.spec_from_file_location('country_sub', SOURCE)
sub = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sub)


def fake_link(*_):
    payload = {'v':1,'name':'Legacy', 'transport':{'url':'https://ingress.invalid/edge', 'quic_url':'https://quic.invalid:7443'},
               'crypto':{'psk':'a'*64,'server_public_key':'b'*64},'camouflage':{},'client_id':'phone'}
    return 'mbox://' + base64.urlsafe_b64encode(json.dumps(payload).encode()).decode().rstrip('=')


def decode(link):
    s=link.split('#')[0][7:]
    return json.loads(base64.urlsafe_b64decode(s+'='*(-len(s)%4)))


def setup(tmp_path,monkeypatch):
    exits=tmp_path/'egress.json'
    db=tmp_path/'db.sqlite'
    con=sqlite3.connect(db)
    con.execute('CREATE TABLE users (id TEXT PRIMARY KEY,enabled INTEGER,expires_at TEXT,max_sessions INTEGER)')
    con.execute('CREATE TABLE subscription_tokens (digest TEXT PRIMARY KEY,user_id TEXT,revoked_at INTEGER)')
    con.execute('INSERT INTO users VALUES (?,?,?,?)',('phone',1,None,4))
    token='B'*43
    con.execute('INSERT INTO subscription_tokens VALUES (?,?,?)',(hashlib.sha256(token.encode()).hexdigest(),'phone',None))
    con.commit();con.close()
    monkeypatch.setattr(sub,'DB',db)
    monkeypatch.setattr(sub,'EGRESS',exits)
    monkeypatch.setattr(sub,'configured_endpoints',lambda:[{'id':'primary','name':'Primary','url':'https://ingress.invalid/edge'}])
    monkeypatch.setattr(sub,'ctl_module',lambda:SimpleNamespace(build_link=fake_link))
    return exits,token


def test_v1_preserved_and_v2_countries_share_ingress(tmp_path,monkeypatch):
    exits,token=setup(tmp_path,monkeypatch)
    exits.write_text(json.dumps({'exits':[
        {'id':'kz','name':'Kazakhstan','upstream':'127.0.0.1:12001'},
        {'id':'dk','name':'Denmark','upstream':'127.0.0.1:12002'}]}))
    old=sub.subscription_document(token,extended=False)
    assert old['version']==1 and [p['id'] for p in old['profiles']]==['primary']
    fresh=sub.subscription_document(token,extended=True)
    assert fresh['version']==2
    assert [p['id'] for p in fresh['profiles']]==['primary','primary_kz','primary_dk']
    assert all(decode(p['link'])['transport']['url']=='https://ingress.invalid/edge' for p in fresh['profiles'])
    assert all('egress_id' not in decode(p['link'])['transport'] for p in old['profiles'])
    assert [decode(p['link'])['transport'].get('egress_id') for p in fresh['profiles']]==[None,'kz','dk']
    assert [decode(p['link'])['crypto']['psk'] for p in fresh['profiles']]==['a'*64]*3


def test_rejects_non_loopback_and_duplicates(tmp_path,monkeypatch):
    exits,_=setup(tmp_path,monkeypatch)
    for address in ['8.8.8.8:1080','example.com:1234','127.0.0.1:0','127.0.0.1:65536']:
        with pytest.raises(ValueError):sub.write_exits([{'id':'kz','name':'Kazakhstan','upstream':address}])
    sub.write_exits([{'id':'kz','name':'Kazakhstan','upstream':'127.0.0.1:12001'}])
    assert sub.configured_exits()[0]['id']=='kz'
    with pytest.raises(ValueError):sub.write_exits([{'id':'kz','name':'A','upstream':'127.0.0.1:12001'}, {'id':'kz','name':'B','upstream':'127.0.0.1:12002'}])


def test_android_and_panel_have_complete_country_flow():
    activity=(ROOT/'android/app/src/main/java/io/mbox/android/MainActivity.java').read_text()
    profile=(ROOT/'android/app/src/main/java/io/mbox/android/ProfileStore.java').read_text()
    subscription=(ROOT/'android/app/src/main/java/io/mbox/android/SubscriptionStore.java').read_text()
    admin=(ROOT/'tools/mbox-admin/mbox_admin.py').read_text()
    gui=(ROOT/'tools/mbox-admin/index.html').read_text()
    assert 'Choose location / profile' in activity
    assert 'panel.addView(chooseLocation' in activity
    assert 'chooseLocationFromHome()' in activity
    assert 'SubscriptionStore.subscriptionLabels(this)' in activity
    assert 'dt.put("egress_id",x.egressId)' in profile
    assert 'application/vnd.mbox.subscription.v2+json' in subscription
    assert 'if (index < 0) throw new Exception' in subscription
    assert "action == 'exit_set'" in admin
    assert "id=\"exitform\"" in gui
