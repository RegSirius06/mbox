import importlib.util
from pathlib import Path

SRC = Path(__file__).resolve().parents[1] / 'tools' / 'singbox_route_audit.py'
SPEC = importlib.util.spec_from_file_location('mbox_singbox_audit', SRC)
MOD = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MOD)


def test_static_graph_and_missing_reference():
    data = {
        'outbounds': [
            {'type': 'selector', 'tag': 'auto', 'outbounds': ['eu', 'dead']},
            {'type': 'vless', 'tag': 'eu', 'server': 'secret.example', 'uuid': 'secret'},
            {'type': 'direct', 'tag': 'direct'},
            {'type': 'socks', 'tag': 'unused-old', 'server': 'old.example'},
        ],
        'route': {
            'final': 'auto',
            'rules': [{'domain': 'example.com', 'outbound': 'direct'}],
        },
    }
    defined, refs, reached = MOD.collect(data)
    assert set(defined) == {'auto', 'eu', 'direct', 'unused-old'}
    assert {'auto', 'eu', 'dead', 'direct'} <= reached
    assert 'unused-old' not in reached
    assert 'dead' not in defined and 'dead' in refs


def test_dns_detour_is_not_falsely_marked_unused():
    data = {'outbounds': [{'type': 'direct', 'tag': 'direct'},
                          {'type': 'socks', 'tag': 'dns-hop'}],
            'route': {'final': 'direct'},
            'dns': {'servers': [{'tag': 'one', 'address': 'tls://secret', 'detour': 'dns-hop'}]}}
    _, _, reached = MOD.collect(data)
    assert 'dns-hop' in reached
