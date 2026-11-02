#!/usr/bin/env python3
from pathlib import Path
import sys
ROOT=Path(__file__).resolve().parents[1]
LIMITS={".go":700,".java":650}
roots=[ROOT/"client",ROOT/"server",ROOT/"android"/"app"/"src"/"main"/"java"]
bad=[]
for base in roots:
    if not base.exists(): continue
    for p in base.rglob("*"):
        if p.suffix not in LIMITS or not p.is_file(): continue
        n=sum(1 for _ in p.open(errors="replace"))
        if n>LIMITS[p.suffix]: bad.append((p.relative_to(ROOT),n,LIMITS[p.suffix]))
if bad:
    for p,n,limit in bad: print(f"{p}: {n} lines > {limit}", file=sys.stderr)
    raise SystemExit(1)
print("source layout: OK")
