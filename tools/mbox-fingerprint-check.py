#!/usr/bin/env python3
"""Small regression checker for Android MBox network-stack logs.

It does not claim to prove DPI indistinguishability. It catches accidental
regressions such as falling back to the Go public HTTPS stack or losing h2 on a
known h2-capable endpoint.
"""
import argparse
import re
import sys
from pathlib import Path


def analyze(text: str, require_h2: bool = False):
    stack_cronet = bool(re.search(r"HTTP_STACK\s+stack=cronet", text))
    stack_go = bool(re.search(r"HTTP_STACK\s+stack=go", text))
    http_ok = re.findall(r"HTTP_OK[^\n]*stack=cronet[^\n]*proto=([^\s]+)[^\n]*alpn=([^\s]+)", text)
    go_ua = "Go-http-client" in text
    noise_ok = "code=NOISE_OK" in text
    tunnel_ok = "code=TUNNEL_ESTABLISHED" in text
    h2 = any(proto.startswith("HTTP/2") or alpn == "h2" for proto, alpn in http_ok)

    failures = []
    if not stack_cronet:
        failures.append("Cronet stack marker not observed")
    if stack_go:
        failures.append("Go public HTTP stack marker observed")
    if go_ua:
        failures.append("Go-http-client marker observed")
    if not noise_ok:
        failures.append("Noise success marker not observed")
    if not tunnel_ok:
        failures.append("tunnel established marker not observed")
    if require_h2 and not h2:
        failures.append("HTTP/2/h2 not observed on an endpoint declared h2-capable")

    return {
        "stack_cronet": stack_cronet,
        "stack_go": stack_go,
        "http_samples": http_ok,
        "h2": h2,
        "noise_ok": noise_ok,
        "tunnel_ok": tunnel_ok,
        "failures": failures,
    }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("log", nargs="?", help="adb logcat text; stdin when omitted")
    ap.add_argument("--require-h2", action="store_true")
    args = ap.parse_args()
    text = Path(args.log).read_text(errors="replace") if args.log else sys.stdin.read()
    result = analyze(text, args.require_h2)
    print(f"cronet={result['stack_cronet']} go_stack={result['stack_go']} h2={result['h2']} noise={result['noise_ok']} tunnel={result['tunnel_ok']}")
    if result["http_samples"]:
        print("http=" + ",".join(f"{p}/{a}" for p, a in result["http_samples"][:8]))
    if result["failures"]:
        for failure in result["failures"]:
            print("FAIL: " + failure, file=sys.stderr)
        return 1
    print("PASS")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
