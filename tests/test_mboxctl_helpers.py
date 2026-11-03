#!/usr/bin/env python3
import importlib.util
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
MODULE=ROOT/'tools'/'mboxctl'/'mboxctl.py'
spec=importlib.util.spec_from_file_location('mboxctl_tested',MODULE)
m=importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


def test_regru_ssh_path_becomes_ftp_candidates():
    got=m.ftp_remote_dir_candidates(
        '/var/www/u1234567/data/www/vpn.example.com/',
        'u1234567',
    )
    assert got == [
        '/var/www/u1234567/data/www/vpn.example.com',
        '/www/vpn.example.com',
        'www/vpn.example.com',
        '/vpn.example.com',
        'vpn.example.com',
    ]


def test_plain_ftp_path_is_preserved():
    assert m.ftp_remote_dir_candidates('/www/example.com','user') == ['/www/example.com']


def test_delete_parser_accepts_many_ids_and_yes():
    args=m.make_parser().parse_args(['user','delete','a','b','-y'])
    assert args.ids == ['a','b']
    assert args.yes is True


def test_cover_simulate_parser():
    args=m.make_parser().parse_args(['cover','simulate','--hours','12','--seed','7'])
    assert args.hours == 12.0
    assert args.seed == 7


if __name__ == '__main__':
    test_regru_ssh_path_becomes_ftp_candidates()
    test_plain_ftp_path_is_preserved()
    test_delete_parser_accepts_many_ids_and_yes()
    test_cover_simulate_parser()
    print('OK')


def test_load_conf_backfills_reliability_and_limits(tmp_path, monkeypatch):
    conf=tmp_path/'mbox.conf'
    conf.write_text('[camouflage]\ndown_max_bytes = 262144\n')
    monkeypatch.setattr(m,'CONF',conf)
    cp=m.load_conf()
    assert cp['camouflage']['up_max_bytes']=='262144'
    assert cp['limits']['global_queued_bytes']=='67108864'


def test_user_limit_parser_supports_unlimited():
    args=m.make_parser().parse_args(['user','limit','alice','--unlimited'])
    assert args.id=='alice' and args.unlimited is True


def test_user_renew_parser():
    args=m.make_parser().parse_args(['user','renew','alice','30'])
    assert args.id=='alice' and args.days==30


def test_backup_prune_parser():
    args=m.make_parser().parse_args(['backup','prune','--keep','5','--dry-run'])
    assert args.keep == 5 and args.dry_run is True


def test_runtime_status_reader(tmp_path, monkeypatch):
    status=tmp_path/'status.json'
    status.write_text('{"active_sessions":3,"clients":{"alice":2}}')
    monkeypatch.setattr(m,'RUNTIME_STATUS',status)
    got=m.read_runtime_status()
    assert got['active_sessions']==3
    assert got['clients']['alice']==2


def test_user_link_parser_network_debug():
    args=m.make_parser().parse_args(['user','link','alice','--network-debug'])
    assert args.id=='alice' and args.network_debug is True


def test_build_app_link_keeps_profile_out_of_query(monkeypatch):
    class FakeCP(dict):
        pass
    cp={'server':{'public_url':'https://vpn.example.com/base'}}
    monkeypatch.setattr(m,'load_conf',lambda:cp)
    monkeypatch.setattr(m,'build_link',lambda uid,name=None,network_debug=False,android_http_stack='cronet',deterministic_transport=False:'mbox://secret-payload#alice')
    got=m.build_app_link('alice')
    assert '?profile=' not in got
    assert '#profile=' in got


def test_user_link_parser_android_http_stack():
    args=m.make_parser().parse_args(['user','link','alice','--android-http-stack','go'])
    assert args.android_http_stack == 'go'
    args=m.make_parser().parse_args(['user','link','alice'])
    assert args.android_http_stack == 'cronet'


def test_user_link_parser_deterministic_transport():
    args=m.make_parser().parse_args(['user','link','alice','--deterministic-transport'])
    assert args.deterministic_transport is True


def test_default_streams_per_session_matches_real_device_calibration():
    assert m.DEFAULT_LIMITS['max_streams_per_session'] == '128'

def test_load_conf_backfills_private_relay_runtime_and_origin_hmac(tmp_path, monkeypatch):
    conf=tmp_path/'mbox.conf'
    conf.write_text('[server]\nlisten=127.0.0.1:1\norigin_hmac_secret=/tmp/key\n[relay]\nmax_slots=4\nmax_body_bytes=2097152\n')
    monkeypatch.setattr(m,'CONF',conf)
    cp=m.load_conf()
    assert cp['relay']['runtime_dir'].startswith('/tmp/mbox-relay-')
    assert cp['server']['origin_hmac_secret']=='/tmp/key'


def test_init_parser_uses_randomized_runtime_dir_by_default():
    args=m.make_parser().parse_args(['init','--public-url','https://x.example/base','--origin-url','https://o.example','--first-user','a'])
    assert args.relay_runtime_dir is None
