"""Subscription labels do not change routing or subscription IDs."""
import importlib.util
import json
from pathlib import Path

MODULE = Path(__file__).resolve().parents[1] / 'tools/mbox-subscription/mbox_subscription.py'
spec = importlib.util.spec_from_file_location('subscription_under_test', MODULE)
sub = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sub)


def test_default_country_label_is_independent_of_origin_allowlist(tmp_path):
    sub.DEFAULT_EXIT_LABEL = tmp_path / 'label'
    assert sub.default_exit_label() == 'Default'
    sub.write_default_exit_label('Дания · каскад')
    assert sub.default_exit_label() == 'Дания · каскад'
    assert sub.DEFAULT_EXIT_LABEL.read_text().strip() == 'Дания · каскад'
    assert not (tmp_path / 'egress.json').exists()
    try:
        sub.write_default_exit_label('DK\nKZ')
    except ValueError:
        pass
    else:
        raise AssertionError('invalid label accepted')


def test_v2_profile_display_name_is_same_inside_mbox_link():
    import base64
    import hashlib
    class FakeCtl:
        @staticmethod
        def build_link(uid):
            raw = json.dumps({'name':'Primary','transport':{'url':'https://old.example'}}).encode()
            return 'mbox://' + base64.urlsafe_b64encode(raw).decode().rstrip('=') + '#primary'
    endpoint = {'id':'primary','name':'Primary','url':'https://relay.example'}
    for label, exit_row in [('Дания · каскад',None),('Казахстан · каскад',{'id':'kz','name':'Казахстан · каскад','upstream':'127.0.0.1:2085'})]:
        link = sub.profile_link(FakeCtl(), 'user', endpoint, exit_row, display_name=label)
        token = link.removeprefix('mbox://').split('#',1)[0]
        obj = json.loads(base64.urlsafe_b64decode(token + '=' * (-len(token) % 4)))
        assert obj['name'] == label
        assert obj['transport']['url'] == endpoint['url']
        assert obj['transport'].get('egress_id') == (None if exit_row is None else 'kz')
