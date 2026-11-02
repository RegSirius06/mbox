import importlib.util
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
MODULE=ROOT/'tools'/'mbox-fingerprint-check.py'
spec=importlib.util.spec_from_file_location('fpcheck',MODULE)
m=importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def test_cronet_h2_log_passes():
    text='''
NET stage=HTTP code=HTTP_STACK stack=cronet version=500.1.0
NET stage=HTTP code=HTTP_OK stack=cronet proto=HTTP/2.0 status=200 tls=cronet alpn=h2
NET stage=NOISE code=NOISE_OK
NET stage=TUNNEL code=TUNNEL_ESTABLISHED
'''
    got=m.analyze(text, True)
    assert got['failures'] == []
    assert got['h2'] is True


def test_go_stack_is_regression():
    text='''
NET stage=HTTP code=HTTP_STACK stack=go
NET stage=NOISE code=NOISE_OK
NET stage=TUNNEL code=TUNNEL_ESTABLISHED
'''
    got=m.analyze(text, False)
    assert any('Go public HTTP stack' in x for x in got['failures'])
    assert any('Cronet stack marker' in x for x in got['failures'])
