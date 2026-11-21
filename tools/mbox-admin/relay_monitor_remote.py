#!/usr/bin/env python3
"""Read only sampler; Python 3.6 compatible. One bounded SSH invocation."""
import json
import os
import signal
import stat
import sys
import time
from pathlib import Path

DURATIONS = (0, 300, 900, 1800, 3600)
INTERVALS = (5, 10, 30)
HZ = os.sysconf('SC_CLK_TCK')
PAGE_KIB = os.sysconf('SC_PAGE_SIZE') / 1024


def snapshot(web, private, proc=Path('/proc')):
    web, private = Path(web), Path(private)
    binary = str(web / 'relay.fcgi')
    cfg = json.loads((web / 'relay.json').read_text())
    slots = cfg['max_slots']
    if type(slots) is not int or not 1 <= slots <= 128:
        raise ValueError('slots')
    locks = Path(cfg['runtime_dir']).resolve()
    if private.resolve() not in locks.parents:
        raise ValueError('locks')
    info = locks.stat()
    if info.st_uid != os.getuid() or not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o077:
        raise ValueError('locks')
    keys = set()
    for i in range(slots):
        try:
            info = (locks / ('slot-%03d.lock' % i)).lstat()
        except FileNotFoundError:
            continue
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise ValueError('lock')
        keys.add((os.major(info.st_dev), os.minor(info.st_dev), info.st_ino))
    active = None
    try:
        held = set()
        for line in (proc / 'locks').read_text().splitlines():
            fields = line.split()
            if len(fields) >= 6 and fields[1:4] == ['FLOCK', 'ADVISORY', 'WRITE']:
                a, b, c = fields[5].split(':')
                held.add((int(a, 16), int(b, 16), int(c)))
        active = len(keys & held)
    except (OSError, ValueError):
        pass
    processes = {}
    incomplete = False
    for p in proc.iterdir():
        if not p.name.isdecimal():
            continue
        try:
            if p.stat().st_uid != os.getuid():
                continue
            raw = (p / 'stat').read_text()
            end = raw.rfind(')')
            if raw[raw.find('(')+1:end] != 'relay.fcgi':
                continue
            try:
                exe = os.readlink(str(p / 'exe'))
            except PermissionError:
                incomplete = True
                continue
            if exe not in (binary, binary + ' (deleted)'):
                continue
            f = raw[end+2:].split()
            processes[(int(p.name), int(f[19]))] = (int(f[11])+int(f[12]), max(0, int(f[21])) * PAGE_KIB)
        except (FileNotFoundError, ProcessLookupError):
            continue
        except (OSError, ValueError, IndexError):
            incomplete = True
    return dict(max_slots=slots, active_slots=active, workers=len(processes),
                rss_kib=round(sum(x[1] for x in processes.values())), incomplete=incomplete), processes


def cpu_delta(previous, current, seconds):
    if previous is None or seconds <= 0:
        return None
    ticks = sum(max(0, current[k][0]-previous[k][0]) for k in current if k in previous)
    return round(100 * ticks / HZ / seconds, 3)


def main():
    # Forced SSH command supplies these fixed paths; stdin supplies only timing.
    if os.environ.get("SSH_ORIGINAL_COMMAND") != "mbox-monitor":
        raise ValueError("command")
    signal.alarm(15)
    request = json.loads(sys.stdin.readline(128))
    if not isinstance(request, dict) or set(request) != {'duration', 'interval'}:
        raise ValueError('request')
    duration, interval = request['duration'], request['interval']
    if type(duration) is not int or type(interval) is not int or duration not in DURATIONS or interval not in INTERVALS:
        raise ValueError('timing')
    signal.alarm(duration + 15)
    start = time.monotonic()
    previous = None
    last = start
    while True:
        now = time.monotonic()
        data, current = snapshot(sys.argv[1], sys.argv[2])
        data.update(elapsed_s=round(now-start, 2), cpu_pct=cpu_delta(previous, current, now-last),
                    turnover=previous is not None and set(previous) != set(current))
        print(json.dumps(data, separators=(',', ':')), flush=True)
        if now-start >= duration or duration == 0:
            break
        previous, last = current, now
        time.sleep(max(0, min(interval, duration-(time.monotonic()-start))))


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, IndexError, TypeError):
        sys.exit('MONITOR_FAILED')
