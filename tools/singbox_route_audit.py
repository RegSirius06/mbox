#!/usr/bin/env python3
"""Read-only sing-box route inventory, deliberately omits server IPs and secrets.

Run on the VPS: python3 singbox_route_audit.py /etc/sing-box/config.json
Never auto-remove an outbound: selectors, DNS detours, route sets and external
configuration includes may refer to it dynamically.
"""
import argparse
import json
from pathlib import Path


def collect(config):
    outbounds = config.get("outbounds", [])
    endpoints = config.get("endpoints", [])
    defined = {}
    for entry in outbounds + endpoints:
        if isinstance(entry, dict):
            tag = entry.get("tag")
            if isinstance(tag, str) and tag:
                defined[tag] = entry.get("type", "unknown")

    refs = {}
    def ref(tag, why):
        if isinstance(tag, str) and tag:
            refs.setdefault(tag, set()).add(why)

    route = config.get("route", {})
    if not isinstance(route, dict):
        route = {}
    ref(route.get("final"), "route.final")

    def walk_route_rules(rules, parent="route.rules"):
        if not isinstance(rules, list):
            return
        for idx, item in enumerate(rules):
            if not isinstance(item, dict):
                continue
            ref(item.get("outbound"), f"{parent}[{idx}].outbound")
            walk_route_rules(item.get("rules"), f"{parent}[{idx}].rules")
    walk_route_rules(route.get("rules"))

    for i, entry in enumerate(outbounds):
        if not isinstance(entry, dict):
            continue
        tag = entry.get("tag", f"outbound[{i}]")
        for value in entry.get("outbounds", []):
            ref(value, f"{tag}.outbounds")
        ref(entry.get("detour"), f"{tag}.detour")

    # DNS detours may also keep an outbound active even without traffic rules.
    dns = config.get("dns", {})
    if isinstance(dns, dict):
        for i, server in enumerate(dns.get("servers", [])):
            if isinstance(server, dict):
                ref(server.get("detour"), f"dns.servers[{i}].detour")
        for i, rule in enumerate(dns.get("rules", [])):
            if isinstance(rule, dict):
                ref(rule.get("outbound"), f"dns.rules[{i}].outbound")

    # Static reachability is conservative. Do not declare any node safely removable.
    roots = set()
    if isinstance(route.get("final"), str) and route["final"]:
        roots.add(route["final"])
    elif outbounds:
        first = outbounds[0]
        if isinstance(first, dict) and isinstance(first.get("tag"), str):
            roots.add(first["tag"])
    for tag, uses in refs.items():
        if any(u.startswith("route.rules") or u.startswith("dns.") for u in uses):
            roots.add(tag)
    edges = {}
    for entry in outbounds:
        if isinstance(entry, dict) and isinstance(entry.get("tag"), str):
            edges[entry["tag"]] = [s for s in entry.get("outbounds", []) if isinstance(s, str)]
            if isinstance(entry.get("detour"), str):
                edges[entry["tag"]].append(entry["detour"])
    reached = set()
    stack = list(roots)
    while stack:
        x = stack.pop()
        if x in reached:
            continue
        reached.add(x)
        stack.extend(edges.get(x, []))
    return defined, refs, reached


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("config", type=Path)
    args = p.parse_args()
    data = json.loads(args.config.read_text())
    defined, refs, reached = collect(data)
    route = data.get("route", {})
    final = route.get("final", "") if isinstance(route, dict) else ""
    first = (data.get("outbounds") or [{}])[0]
    if not final and isinstance(first, dict):
        final = first.get("tag", "")
    print(f"Effective default outbound tag: {final!r}")
    if defined.get(final) == "direct":
        print("WARNING: default outbound is direct; check expected exit geography")
    print(f"Defined outbound/endpoint tags: {len(defined)}")
    for tag, typ in sorted(defined.items()):
        status = "REFERENCED" if tag in reached else "REVIEW_UNUSED"
        print(f"  {status:<13} tag={tag!r} type={typ}")
    dangling = sorted((tag, uses) for tag, uses in refs.items() if tag not in defined)
    print(f"Missing outbound references: {len(dangling)}")
    for tag, uses in dangling:
        print(f"  tag={tag!r} referenced_by={','.join(sorted(uses))}")
    print("Note: REVIEW_UNUSED is NOT proof a node is safe to delete.")
    print("Server addresses, passwords, UUIDs and routing rule values are not printed.")


if __name__ == "__main__":
    main()
