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
