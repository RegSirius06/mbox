#!/usr/bin/env python3
"""Probe a TEST Android MBox client's SOCKS endpoint through adb forward.

On Arch with an Android device connected over adb:
  adb forward tcp:19090 tcp:10890
  python3 tools/android_socks_probe.py --port 19090
  adb forward --remove tcp:19090

No VPN keys or profile material are read. Does not probe user destinations.
"""
import argparse
import socket


def exact(sock, n):
    buf = bytearray()
    while len(buf) < n:
        piece = sock.recv(n - len(buf))
        if not piece:
            raise RuntimeError("connection closed before SOCKS reply")
        buf.extend(piece)
    return bytes(buf)


def probe(host, port, cmd):
    with socket.create_connection((host, port), timeout=8) as sock:
        sock.settimeout(8)
        # Split hello to test bounded greeting parsing.
        sock.sendall(b"\x05")
        sock.sendall(b"\x01\x00")
        if exact(sock, 2) != b"\x05\x00":
            raise RuntimeError("SOCKS5 no-auth greeting failed")
        # SOCKS5 request: 127.0.0.1:53. Never forwarded on disallowed cmd.
        request = bytes([5, cmd, 0, 1, 127, 0, 0, 1, 0, 53])
        sock.sendall(request[:3])
        sock.sendall(request[3:])
        response = exact(sock, 10)
        if response[:2] != b"\x05\x07":
            raise RuntimeError(f"cmd=0x{cmd:02x} got rep={response[:2].hex()}, expected 0507")
        print(f"PASS: SOCKS cmd 0x{cmd:02x} rejected with RFC1928 REP 07")


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=19090)
    a = ap.parse_args()
    probe(a.host, a.port, 2)  # BIND
    probe(a.host, a.port, 3)  # UDP ASSOCIATE
    print("PASS: local unsupported SOCKS commands rejected")


if __name__ == "__main__":
    main()
