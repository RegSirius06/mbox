#!/usr/bin/env python3
import argparse
import base64
import configparser
import csv
import datetime as dt
import ftplib
import getpass
import hashlib
import json
import os
import re
import random
import time
import secrets
import shlex
import shutil
import sqlite3
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path
from urllib.parse import urlparse

ETC = Path(os.environ.get("MBOX_ETC", "/etc/mbox"))
STATE = Path(os.environ.get("MBOX_STATE", "/var/lib/mbox"))
CONF = ETC / "mbox.conf"
RUNTIME = ETC / "server.json"
DB = STATE / "users.db"
REGRU = STATE / "regru" / "current"
ORIGIN = STATE / "origin" / "current"
RELAY_TEMPLATE = Path(os.environ.get("MBOX_RELAY_TEMPLATE", "/usr/local/lib/mbox/relay.fcgi"))
SERVICE = "mbox-server.service"
KEY_TOOL = Path(os.environ.get("MBOX_KEY_TOOL", "/usr/local/bin/mbox-key"))
CLIENT_ID_RE = re.compile(r"^[A-Za-z0-9._-]{1,63}$")

DEFAULT_CAMO = {
    "open_path": "/api/v1/session",
    "up_path": "/api/v1/events/",
    "down_path": "/api/v1/sync/",
    "close_path": "/api/v1/session/",
    "content_type": "application/json",
    "down_max_bytes": "262144",
    "padding_classes": "512,1024,2048,4096,8192,16384,32768,65536,131072,262144,524288",
    "padding_extra_class_percent": "20",
    "idle_min_ms": "45000",
    "idle_max_ms": "90000",
    "wake_min_ms": "100",
    "wake_max_ms": "220",
    "active_jitter_percent": "20",
    "failure_timeout_ms": "30000",
    "retry_min_ms": "300",
    "retry_max_ms": "5000",
}


def eprint(msg):
    print(msg, file=sys.stderr)

DEFAULT_COVER = {
    "enabled": "false",
    "path": "/api/v1/assets/",
    "status_file": "/run/mbox-cover-status.json",
    "idle_after_ms": "120000",
    "start_grace_min_ms": "60000",
    "start_grace_max_ms": "180000",
    "max_rps": "4",
    "max_requests_per_hour": "1200",
    "request_timeout_ms": "8000",
    "quiet_weight": "35",
    "casual_weight": "40",
    "active_weight": "20",
    "burst_weight": "5",
}


def die(msg, code=1):
    print(f"ERROR: {msg}", file=sys.stderr)
    raise SystemExit(code)


def run(cmd, check=True, capture=False):
    return subprocess.run(cmd, check=check, text=True,
                          stdout=subprocess.PIPE if capture else None,
                          stderr=subprocess.PIPE if capture else None)


def now():
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat()


def require_root():
    if os.geteuid() != 0:
        die("run this command as root")


def mkdirs():
    ETC.mkdir(parents=True, exist_ok=True)
    STATE.mkdir(parents=True, exist_ok=True)
    REGRU.mkdir(parents=True, exist_ok=True)
    os.chmod(ETC, 0o700)
    os.chmod(STATE, 0o700)


def load_conf(required=True):
    cp = configparser.ConfigParser(interpolation=None)
    if not CONF.exists():
        if required:
            die(f"configuration not found: {CONF}; run 'mboxctl init'")
        return cp
    cp.read(CONF)
    if "cover" not in cp:
        cp["cover"] = DEFAULT_COVER.copy()
    else:
        for k, v in DEFAULT_COVER.items():
            if k not in cp["cover"]:
                cp["cover"][k] = v
    return cp


def save_conf(cp):
    mkdirs()
    tmp = CONF.with_suffix(".conf.tmp")
    with tmp.open("w") as f:
        cp.write(f)
    os.chmod(tmp, 0o600)
    os.replace(tmp, CONF)


def db_connect():
    mkdirs()
    con = sqlite3.connect(DB)
    con.row_factory = sqlite3.Row
    con.execute("PRAGMA journal_mode=WAL")
    con.execute("PRAGMA foreign_keys=ON")
    con.executescript("""
    CREATE TABLE IF NOT EXISTS users (
      id TEXT PRIMARY KEY,
      psk BLOB NOT NULL CHECK(length(psk)=32),
      enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
      label TEXT NOT NULL DEFAULT '',
      note TEXT NOT NULL DEFAULT '',
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL,
      rotated_at TEXT,
      revoked_at TEXT
    );
    CREATE INDEX IF NOT EXISTS idx_users_enabled ON users(enabled);
    """)
    con.commit()
    try:
        os.chmod(DB, 0o600)
    except FileNotFoundError:
        pass
    return con


def validate_client_id(x):
    if not CLIENT_ID_RE.fullmatch(x or ""):
        die(f"invalid client id {x!r}; allowed: A-Z a-z 0-9 . _ -; length 1..63")


def read_hex32(path):
    raw = Path(path).read_text().strip()
    try:
        b = bytes.fromhex(raw)
    except ValueError:
        die(f"{path}: invalid hex")
    if len(b) != 32:
        die(f"{path}: expected 32-byte hex key")
    return b


def ensure_keys(cp):
    priv = Path(cp["server"]["server_private_key"])
    pub = Path(cp["server"]["server_public_key"])
    priv.parent.mkdir(parents=True, exist_ok=True)
    if not priv.exists():
        if not KEY_TOOL.exists():
            die(f"mbox-key not installed: {KEY_TOOL}")
        run([str(KEY_TOOL), "-mode", "generate", "-private", str(priv), "-public", str(pub)])
    else:
        run([str(KEY_TOOL), "-mode", "derive", "-private", str(priv), "-public", str(pub)])
    os.chmod(priv, 0o600)
    os.chmod(pub, 0o644)


def camo_dict(cp, include_timing=True):
    c = cp["camouflage"]
    d = {
        "open_path": c["open_path"],
        "up_path": c["up_path"],
        "down_path": c["down_path"],
        "close_path": c["close_path"],
        "content_type": c["content_type"],
        "request_headers": {"Accept": c["content_type"]},
        "response_headers": {"Cache-Control": "no-store"},
        "down_max_bytes": int(c["down_max_bytes"]),
        "padding_classes": [int(x) for x in c["padding_classes"].split(",") if x.strip()],
        "padding_extra_class_percent": int(c["padding_extra_class_percent"]),
    }
    if include_timing:
        d["timing"] = {
            "idle_min_ms": int(c["idle_min_ms"]),
            "idle_max_ms": int(c["idle_max_ms"]),
            "wake_min_ms": int(c["wake_min_ms"]),
            "wake_max_ms": int(c["wake_max_ms"]),
            "active_jitter_percent": int(c["active_jitter_percent"]),
        }
        d["recovery"] = {
            "failure_timeout_ms": int(c["failure_timeout_ms"]),
            "retry_min_ms": int(c["retry_min_ms"]),
            "retry_max_ms": int(c["retry_max_ms"]),
        }
    return d


def render_runtime(reload_auth=False, restart=False):
    cp = load_conf()
    validate_config(cp)
    con = db_connect()
    clients = {}
    for row in con.execute("SELECT id, psk FROM users WHERE enabled=1 ORDER BY id"):
        clients[row["id"]] = {"psk": bytes(row["psk"]).hex()}
    obj = {
        "camouflage": camo_dict(cp, include_timing=False),
        "transport": {
            "listen": cp["server"]["listen"],
            "upstream": cp["server"]["upstream"],
            "base_path": cp["server"]["base_path"],
        },
        "crypto": {
            "psk_file": cp["server"].get("legacy_psk_file", ""),
            "server_private_key_file": cp["server"]["server_private_key"],
            "clients": clients,
        },
        "cover": {
            "enabled": cp.getboolean("cover", "enabled"),
            "public_url": cp["server"]["public_url"],
            "path": cp["cover"]["path"],
            "status_file": cp["cover"]["status_file"],
            "idle_after_ms": int(cp["cover"]["idle_after_ms"]),
            "start_grace_min_ms": int(cp["cover"]["start_grace_min_ms"]),
            "start_grace_max_ms": int(cp["cover"]["start_grace_max_ms"]),
            "max_rps": int(cp["cover"]["max_rps"]),
            "max_requests_per_hour": int(cp["cover"]["max_requests_per_hour"]),
            "request_timeout_ms": int(cp["cover"]["request_timeout_ms"]),
            "quiet_weight": int(cp["cover"]["quiet_weight"]),
            "casual_weight": int(cp["cover"]["casual_weight"]),
            "active_weight": int(cp["cover"]["active_weight"]),
            "burst_weight": int(cp["cover"]["burst_weight"]),
        },
    }
    mkdirs()
    tmp = RUNTIME.with_suffix(".json.tmp")
    tmp.write_text(json.dumps(obj, indent=2) + "\n")
    os.chmod(tmp, 0o600)
    os.replace(tmp, RUNTIME)
    if restart:
        service_action("restart", quiet=True)
    elif reload_auth:
        service_hup()


def service_active():
    p = subprocess.run(["systemctl", "is-active", "--quiet", SERVICE])
    return p.returncode == 0


def service_hup():
    """Reload auth safely.

    New MBox servers handle SIGHUP in-process.  Older pre-repository
    production binaries did not, so a raw HUP could terminate the daemon.
    Use systemd ExecReload and verify that the service survived.  If an old
    binary exits, immediately restart it so the newly installed binary takes
    over instead of leaving Caddy with a dead upstream.
    """
    if not service_active():
        return
    p = run(["systemctl", "reload", SERVICE], check=False)
    # systemctl may return before the process has handled the signal.
    import time
    time.sleep(0.20)
    if p.returncode != 0 or not service_active():
        eprint("warning: auth reload stopped/failed; restarting mbox-server")
        service_action("restart", quiet=True)
        time.sleep(0.20)
        if not service_active():
            die("mbox-server failed to recover after auth reload")


def service_action(action, quiet=False):
    allowed = {"start", "stop", "restart", "status"}
    if action not in allowed:
        die("invalid service action")
    cmd = ["systemctl", action, SERVICE]
    if action == "status":
        cmd += ["--no-pager"]
    p = run(cmd, check=False)
    if p.returncode != 0 and action != "status" and not quiet:
        die(f"systemctl {action} failed")
    return p.returncode


def validate_config(cp):
    needed = [("server", k) for k in ["listen", "upstream", "base_path", "public_url", "origin_url", "server_private_key", "server_public_key"]]
    needed += [("relay", k) for k in ["max_slots", "max_body_bytes", "slot_lock_prefix"]]
    needed += [("camouflage", k) for k in DEFAULT_CAMO]
    needed += [("cover", k) for k in DEFAULT_COVER]
    for section, key in needed:
        if section not in cp or not cp[section].get(key, "").strip():
            die(f"missing config: {section}.{key}")
    base = cp["server"]["base_path"]
    if not base.startswith("/") or base.endswith("/"):
        die("server.base_path must start with / and must not end with /")
    public = urlparse(cp["server"]["public_url"])
    origin = urlparse(cp["server"]["origin_url"])
    if public.scheme != "https" or not public.netloc:
        die("server.public_url must be https://...")
    if origin.scheme != "https" or not origin.netloc:
        die("server.origin_url must be https://...")
    if not cp["server"]["public_url"].rstrip("/").endswith(base):
        die("server.public_url must end with server.base_path")
    if not (1 <= int(cp["relay"]["max_slots"]) <= 128):
        die("relay.max_slots out of range")
    if int(cp["camouflage"]["idle_min_ms"]) > int(cp["camouflage"]["idle_max_ms"]):
        die("camouflage idle range invalid")
    cv=cp["cover"]
    try:
        cv_enabled=cp.getboolean("cover","enabled")
    except ValueError:
        die("cover.enabled must be true or false")
    if not cv["path"].startswith("/") or not cv["path"].endswith("/"):
        die("cover.path must start and end with /")
    if int(cv["idle_after_ms"]) < 10000:
        die("cover.idle_after_ms must be >= 10000")
    if int(cv["start_grace_min_ms"]) > int(cv["start_grace_max_ms"]):
        die("cover grace range invalid")
    if not (1 <= int(cv["max_rps"]) <= 50):
        die("cover.max_rps out of range 1..50")
    if not (1 <= int(cv["max_requests_per_hour"]) <= 100000):
        die("cover.max_requests_per_hour out of range")
    weights=sum(int(cv[k]) for k in ["quiet_weight","casual_weight","active_weight","burst_weight"])
    if weights <= 0:
        die("cover profile weights sum to zero")
    return True


def prompt(label, default=None):
    suffix = f" [{default}]" if default is not None else ""
    v = input(f"{label}{suffix}: ").strip()
    return v or (default if default is not None else "")


def _safe_rmtree(path):
    path = Path(path)
    try:
        resolved = path.resolve()
    except FileNotFoundError:
        resolved = path.absolute()
    # Refuse obviously dangerous roots.  MBOX_ETC/MBOX_STATE are expected to
    # be dedicated MBox directories such as /etc/mbox, /var/lib/mbox or a
    # deeper temporary test directory.
    if len(resolved.parts) < 3 or resolved == Path("/"):
        die(f"refusing to remove unsafe path: {resolved}")
    if path.is_symlink():
        path.unlink(missing_ok=True)
    elif path.exists():
        shutil.rmtree(path)


def _reinit_backup():
    backup_dir = Path(os.environ.get("MBOX_BACKUP_DIR", "/root"))
    backup_dir.mkdir(parents=True, exist_ok=True)
    stamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S-%f")
    out = backup_dir / f"mbox-pre-reinit-{stamp}.tar.gz"
    with tarfile.open(out, "w:gz") as tf:
        if ETC.exists():
            tf.add(ETC, arcname="etc-mbox")
        if STATE.exists():
            tf.add(STATE, arcname="state-mbox")
    os.chmod(out, 0o600)
    return out


def _restore_reinit_backup(path):
    with tempfile.TemporaryDirectory(prefix="mbox-reinit-restore-") as td:
        td = Path(td)
        with tarfile.open(path, "r:gz") as tf:
            tf.extractall(td, filter="data")
        _safe_rmtree(ETC)
        _safe_rmtree(STATE)
        etc_src = td / "etc-mbox"
        state_src = td / "state-mbox"
        if etc_src.exists():
            ETC.parent.mkdir(parents=True, exist_ok=True)
            shutil.copytree(etc_src, ETC)
        if state_src.exists():
            STATE.parent.mkdir(parents=True, exist_ok=True)
            shutil.copytree(state_src, STATE)


def init_cmd(args):
    require_root()
    existing = (
        CONF.exists() or RUNTIME.exists() or DB.exists() or
        (ETC / "server.key").exists() or (ETC / "server.pub").exists()
    )
    if existing and not args.force:
        die(f"{CONF} already exists (use --force to reinitialize)")
    base = args.base_path or "/edge/" + secrets.token_hex(16)
    public = args.public_url or prompt("Public frontend URL (without trailing slash, may include base path)", "")
    if not public:
        host = prompt("Public frontend hostname, e.g. vpn.example.com")
        public = "https://" + host + base
    elif urlparse(public).path in ("", "/"):
        public = public.rstrip("/") + base
    origin = args.origin_url or prompt("Origin URL on RU server, e.g. https://origin.example.com")
    if not origin:
        die("origin URL is required")
    first = args.first_user or prompt("Initial client id", "android-main")
    validate_client_id(first)

    backup = None
    previous_service_active = False
    if args.force and existing:
        # Stop writers before archiving SQLite/WAL and replacing the state.
        previous_service_active = service_active()
        if previous_service_active:
            p = run(["systemctl", "stop", SERVICE], check=False)
            if p.returncode != 0:
                die("cannot stop mbox-server before forced reinitialization")
        backup = _reinit_backup()
        print(f"Existing MBox state backed up to: {backup}")
        _safe_rmtree(ETC)
        _safe_rmtree(STATE)

    cp = configparser.ConfigParser(interpolation=None)
    cp["server"] = {
        "listen": args.listen,
        "upstream": args.upstream,
        "base_path": base,
        "public_url": public.rstrip("/"),
        "origin_url": origin.rstrip("/"),
        "server_private_key": str(ETC / "server.key"),
        "server_public_key": str(ETC / "server.pub"),
        "legacy_psk_file": "",
    }
    cp["relay"] = {
        "max_slots": str(args.max_slots),
        "max_body_bytes": str(args.max_body_bytes),
        "slot_lock_prefix": args.slot_lock_prefix,
    }
    cp["camouflage"] = DEFAULT_CAMO.copy()
    cp["cover"] = DEFAULT_COVER.copy()
    try:
        save_conf(cp); ensure_keys(cp); db_connect().close()
        con = db_connect()
        con.execute("INSERT INTO users(id,psk,enabled,label,note,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",
                    (first, secrets.token_bytes(32), 1, first, "initial user", now(), now()))
        con.commit(); con.close()
        render_runtime()
        generate_relay_files(); generate_origin_files()
        run(["systemctl", "daemon-reload"], check=False)
        p = run(["systemctl", "enable", "--now", SERVICE], check=False)
        if p.returncode != 0:
            die("failed to enable/start mbox-server after initialization")
    except BaseException:
        if backup is not None:
            eprint(f"reinitialization failed; restoring previous state from {backup}")
            try:
                _restore_reinit_backup(backup)
                run(["systemctl", "daemon-reload"], check=False)
                if previous_service_active:
                    run(["systemctl", "start", SERVICE], check=False)
                eprint("previous MBox state restored")
            except Exception as restore_error:
                eprint(f"CRITICAL: automatic restore failed: {restore_error}")
        raise
    print("MBox initialized.")
    if backup is not None:
        print(f"Previous state backup: {backup}")
    print(f"Config: {CONF}")
    print(f"Users DB: {DB}")
    print(f"REG.RU files: {REGRU}")
    print(f"Initial link: sudo mboxctl user link {first}")


def infer_legacy_urls(obj):
    public = None; origin = None
    candidates = [Path("/root/mbox-production-out/client-public.json"), Path("/root/mbox1-config-dev/config/client.json")]
    for p in candidates:
        if p.exists():
            try:
                public = json.loads(p.read_text()).get("transport", {}).get("url") or public
            except Exception:
                pass
    for p in [Path("/root/mbox-production-out/relay.json")]:
        if p.exists():
            try:
                origin = json.loads(p.read_text()).get("target") or origin
            except Exception:
                pass
    return public, origin


def migrate_legacy(args):
    require_root(); mkdirs()
    src = Path(args.server_json)
    if not src.exists(): die(f"legacy config not found: {src}")
    obj = json.loads(src.read_text())
    base = obj["transport"]["base_path"]
    public0, origin0 = infer_legacy_urls(obj)
    public = args.public_url or public0
    origin = args.origin_url or origin0
    if not public:
        public = prompt("Public frontend URL", "")
    if not origin:
        origin = prompt("Origin URL", "")
    if not public or not origin: die("public and origin URLs are required")
    cp = configparser.ConfigParser(interpolation=None)
    cp["server"] = {
        "listen": obj["transport"]["listen"],
        "upstream": obj["transport"]["upstream"],
        "base_path": base,
        "public_url": public.rstrip("/"),
        "origin_url": origin.rstrip("/"),
        "server_private_key": obj["crypto"]["server_private_key_file"],
        "server_public_key": str(ETC / "server.pub"),
        "legacy_psk_file": obj.get("crypto", {}).get("psk_file", ""),
    }
    relay_obj = {}
    rp = Path("/root/mbox-production-out/relay.json")
    if rp.exists():
        try: relay_obj = json.loads(rp.read_text())
        except Exception: relay_obj = {}
    cp["relay"] = {
        "max_slots": str(relay_obj.get("max_slots", 4)),
        "max_body_bytes": str(relay_obj.get("max_body_bytes", 2097152)),
        "slot_lock_prefix": relay_obj.get("slot_lock_prefix", "/tmp/mbox-relay"),
    }
    cam = DEFAULT_CAMO.copy()
    oldc = obj.get("camouflage", {})
    for k in ["open_path","up_path","down_path","close_path","content_type","down_max_bytes","padding_extra_class_percent"]:
        if k in oldc: cam[k] = str(oldc[k])
    if "padding_classes" in oldc: cam["padding_classes"] = ",".join(map(str, oldc["padding_classes"]))
    # timing lives in client config, if available
    for p in [Path("/root/mbox-production-out/client-public.json"), Path("/root/mbox1-config-dev/config/client.json")]:
        if p.exists():
            try:
                co = json.loads(p.read_text()).get("camouflage", {})
                for k,v in co.get("timing",{}).items(): cam[k]=str(v)
                for k,v in co.get("recovery",{}).items(): cam[k]=str(v)
                break
            except Exception: pass
    cp["camouflage"] = cam
    cp["cover"] = DEFAULT_COVER.copy()
    save_conf(cp); ensure_keys(cp)
    con = db_connect()
    imported = 0
    with con:
        for uid, ent in obj.get("crypto", {}).get("clients", {}).items():
            validate_client_id(uid)
            if ent.get("psk"):
                secret = bytes.fromhex(ent["psk"])
            elif ent.get("psk_file"):
                secret = read_hex32(ent["psk_file"])
            else:
                continue
            con.execute("INSERT OR REPLACE INTO users(id,psk,enabled,label,note,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",
                        (uid, secret, 1, uid, "migrated from legacy config", now(), now()))
            imported += 1
    con.close(); render_runtime(); generate_relay_files(); generate_origin_files()
    run(["systemctl", "daemon-reload"], check=False)
    run(["systemctl", "enable", SERVICE], check=False)
    # Migration may be replacing a pre-repository binary which does not
    # understand SIGHUP.  Always start the freshly installed server binary.
    if service_action("restart", quiet=True) != 0 or not service_active():
        die("mbox-server failed to restart after legacy migration")
    print(f"Migrated {imported} users from {src}")
    print(f"New admin config: {CONF}")
    print(f"SQLite DB: {DB}")
    print(f"REG.RU artifacts: {REGRU}")


def user_add(args):
    require_root(); load_conf()
    ids = list(args.ids)
    if args.count:
        width = args.width or max(3, len(str(args.count)))
        ids += [f"{args.prefix}{i:0{width}d}" for i in range(1, args.count+1)]
    if not ids: die("provide user ids or --count")
    for uid in ids: validate_client_id(uid)
    if len(ids) != len(set(ids)): die("duplicate user ids in request")
    con = db_connect(); ts=now()
    try:
        with con:
            for uid in ids:
                con.execute("INSERT INTO users(id,psk,enabled,label,note,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",
                            (uid, secrets.token_bytes(32), 1, uid, args.note or "", ts, ts))
    except sqlite3.IntegrityError as e:
        die(f"user already exists: {e}")
    finally: con.close()
    render_runtime(reload_auth=True)
    for uid in ids: print(uid)
    print(f"Created {len(ids)} user(s).")


def user_import(args):
    require_root(); load_conf(); rows=[]
    with open(args.csv_file, newline="") as f:
        r=csv.DictReader(f)
        if not r.fieldnames or "id" not in r.fieldnames: die("CSV must contain an 'id' column")
        for row in r:
            uid=(row.get("id") or "").strip(); validate_client_id(uid)
            rows.append((uid,(row.get("label") or uid).strip(),(row.get("note") or "").strip()))
    con=db_connect(); ts=now()
    try:
        with con:
            for uid,label,note in rows:
                con.execute("INSERT INTO users(id,psk,enabled,label,note,created_at,updated_at) VALUES(?,?,?,?,?,?,?)",
                            (uid,secrets.token_bytes(32),1,label,note,ts,ts))
    except sqlite3.IntegrityError as e: die(str(e))
    finally: con.close()
    render_runtime(reload_auth=True); print(f"Imported {len(rows)} user(s).")


def user_list(args):
    con=db_connect()
    q="SELECT id,enabled,label,created_at,rotated_at,revoked_at FROM users"
    cond=[]
    if args.enabled: cond.append("enabled=1")
    if args.revoked: cond.append("revoked_at IS NOT NULL")
    if cond: q += " WHERE "+" AND ".join(cond)
    q += " ORDER BY id"
    rows=con.execute(q).fetchall(); con.close()
    print(f"{'ID':<28} {'STATE':<9} {'LABEL':<22} CREATED")
    for r in rows:
        state="enabled" if r["enabled"] else ("revoked" if r["revoked_at"] else "disabled")
        print(f"{r['id']:<28} {state:<9} {r['label'][:22]:<22} {r['created_at']}")
    print(f"Total: {len(rows)}")


def get_user(uid):
    validate_client_id(uid); con=db_connect(); r=con.execute("SELECT * FROM users WHERE id=?",(uid,)).fetchone(); con.close()
    if not r: die(f"user not found: {uid}")
    return r


def user_show(args):
    r=get_user(args.id)
    d=dict(r); d.pop("psk",None); d["state"]="enabled" if r["enabled"] else ("revoked" if r["revoked_at"] else "disabled")
    print(json.dumps(d,indent=2))


def user_state(args, mode):
    require_root(); get_user(args.id); con=db_connect(); ts=now()
    with con:
        if mode=="enable": con.execute("UPDATE users SET enabled=1,revoked_at=NULL,updated_at=? WHERE id=?",(ts,args.id))
        elif mode=="disable": con.execute("UPDATE users SET enabled=0,updated_at=? WHERE id=?",(ts,args.id))
        elif mode=="revoke": con.execute("UPDATE users SET enabled=0,revoked_at=?,updated_at=? WHERE id=?",(ts,ts,args.id))
    con.close(); render_runtime(reload_auth=True); print(f"{mode}: {args.id}")


def user_rotate(args):
    require_root(); get_user(args.id); con=db_connect(); ts=now()
    with con:
        con.execute("UPDATE users SET psk=?,enabled=1,rotated_at=?,revoked_at=NULL,updated_at=? WHERE id=?",
                    (secrets.token_bytes(32),ts,ts,args.id))
    con.close(); render_runtime(reload_auth=True); print(f"Rotated: {args.id}")
    if args.print_link: print(build_link(args.id, args.name))


def user_delete(args):
    require_root()
    ids=list(dict.fromkeys(args.ids))
    if not ids:
        die("specify at least one user id")
    rows=[]
    for uid in ids:
        rows.append(get_user(uid))
    assume_yes=bool(getattr(args,"yes",False) or getattr(args,"purge",False))
    if not assume_yes:
        enabled=[r["id"] for r in rows if r["enabled"]]
        print("Permanent deletion removes the user and PSK from SQLite.")
        if enabled:
            print("WARNING: enabled user(s): "+", ".join(enabled))
        print("Delete: "+", ".join(ids))
        answer=input("Continue? [y/N]: ").strip().lower()
        if answer not in {"y","yes"}:
            print("Cancelled.")
            return
    con=db_connect()
    try:
        with con:
            con.executemany("DELETE FROM users WHERE id=?",[(uid,) for uid in ids])
    finally:
        con.close()
    render_runtime(reload_auth=True)
    for uid in ids:
        print(f"Deleted: {uid}")
    print(f"Deleted {len(ids)} user(s).")


def build_link(uid, name=None):
    cp=load_conf(); r=get_user(uid)
    pub=read_hex32(cp["server"]["server_public_key"]).hex()
    payload={
        "v":1,
        "name": name or r["label"] or uid,
        "client_id":uid,
        "transport":{"url":cp["server"]["public_url"]},
        "crypto":{"psk":bytes(r["psk"]).hex(),"server_public_key":pub},
        "camouflage":camo_dict(cp,include_timing=True),
    }
    raw=json.dumps(payload,separators=(",",":"),ensure_ascii=False).encode()
    return "mbox://"+base64.urlsafe_b64encode(raw).decode().rstrip("=")+"#"+uid


def user_link(args):
    print(build_link(args.id,args.name))


def user_qr(args):
    link=build_link(args.id,args.name)
    if shutil.which("qrencode"):
        run(["qrencode","-t","ANSIUTF8",link])
    else:
        die("qrencode is not installed; use 'mboxctl user link' or install qrencode")


def export_one(uid, outdir, include_qr=True):
    outdir.mkdir(parents=True,exist_ok=True)
    link=build_link(uid)
    lp=outdir/f"{uid}.mbox"; lp.write_text(link+"\n"); os.chmod(lp,0o600)
    if include_qr and shutil.which("qrencode"):
        run(["qrencode","-o",str(outdir/f"{uid}.png"),link])
    return lp


def user_export(args):
    require_root(); ids=[]
    if args.all_enabled:
        con=db_connect(); ids=[r[0] for r in con.execute("SELECT id FROM users WHERE enabled=1 ORDER BY id")]; con.close()
    else: ids=args.ids
    if not ids: die("no users selected")
    out=Path(args.output)
    if str(out).endswith(".tar.gz"):
        with tempfile.TemporaryDirectory(prefix="mbox-export-") as td:
            d=Path(td)/"provisioning"; d.mkdir()
            for uid in ids: export_one(uid,d,not args.no_qr)
            with tarfile.open(out,"w:gz") as tf: tf.add(d,arcname="provisioning")
        os.chmod(out,0o600)
    else:
        for uid in ids: export_one(uid,out,not args.no_qr)
    print(f"Exported {len(ids)} profile(s) to {out}")


def user_count(args):
    con=db_connect()
    total=con.execute("SELECT count(*) FROM users").fetchone()[0]
    enabled=con.execute("SELECT count(*) FROM users WHERE enabled=1").fetchone()[0]
    revoked=con.execute("SELECT count(*) FROM users WHERE revoked_at IS NOT NULL").fetchone()[0]
    con.close()
    print(f"total={total} enabled={enabled} revoked={revoked} disabled={total-enabled}")

def user_rename(args):
    require_root(); validate_client_id(args.new_id); get_user(args.id)
    con=db_connect(); ts=now()
    try:
        with con:
            con.execute("UPDATE users SET id=?, label=CASE WHEN label=? THEN ? ELSE label END, updated_at=? WHERE id=?",
                        (args.new_id,args.id,args.new_id,ts,args.id))
    except sqlite3.IntegrityError:
        die(f"user already exists: {args.new_id}")
    finally:
        con.close()
    render_runtime(reload_auth=True); print(f"Renamed: {args.id} -> {args.new_id}")

def logs_cmd(args):
    cmd=["journalctl","-u",SERVICE,"--no-pager"]
    if args.follow: cmd.append("-f")
    if args.lines: cmd += ["-n",str(args.lines)]
    return subprocess.call(cmd)

def db_check(args):
    con=db_connect(); print(con.execute("PRAGMA integrity_check").fetchone()[0]); con.close()

def db_vacuum(args):
    require_root(); con=db_connect(); con.execute("VACUUM"); con.close(); print("VACUUM complete")

def backup_verify(args):
    p=Path(args.path)
    if not p.exists(): die(f"backup not found: {p}")
    try:
        with tarfile.open(p,"r:gz") as tf:
            names=tf.getnames()
            if not any(n.startswith("etc-mbox") for n in names): die("backup does not contain etc-mbox")
        print("OK")
    except tarfile.TarError as e:
        die(f"invalid backup: {e}")

def link_inspect(args):
    text=Path(args.path).read_text().strip() if args.path != "-" else sys.stdin.read().strip()
    if not text.lower().startswith("mbox://"): die("not an mbox:// link")
    body=text.split("#",1)[0][len("mbox://"):]
    body += "="*((4-len(body)%4)%4)
    try: obj=json.loads(base64.urlsafe_b64decode(body))
    except Exception as e: die(f"invalid link: {e}")
    if args.redact and "crypto" in obj:
        if "psk" in obj["crypto"]: obj["crypto"]["psk"]="<redacted>"
    print(json.dumps(obj,indent=2,ensure_ascii=False))

def config_upgrade(args):
    require_root(); cp=load_conf(); validate_config(cp); save_conf(cp)
    print(f"config upgraded: {CONF}")


def config_show(args):
    print(CONF.read_text())


def config_get(args):
    cp=load_conf(); sec,key=args.key.split(".",1) if "." in args.key else (None,None)
    if not sec or sec not in cp or key not in cp[sec]: die("key must be section.key and exist")
    print(cp[sec][key])


def config_set(args):
    require_root(); cp=load_conf();
    if "." not in args.key: die("key must be section.key")
    sec,key=args.key.split(".",1)
    if sec not in cp or key not in cp[sec]: die("unknown config key")
    cp[sec][key]=args.value; validate_config(cp); save_conf(cp); render_runtime(restart=True); generate_relay_files(); generate_origin_files()
    print(f"{args.key}={args.value}")


def cover_set_enabled(enabled):
    require_root()
    cp=load_conf()
    cp["cover"]["enabled"]="true" if enabled else "false"
    validate_config(cp); save_conf(cp); render_runtime(restart=True)
    print("cover enabled" if enabled else "cover disabled")


def cover_status(args):
    cp=load_conf(); validate_config(cp)
    print(f"configured={'enabled' if cp.getboolean('cover','enabled') else 'disabled'}")
    print(f"public_url={cp['server']['public_url']}")
    print(f"path={cp['cover']['path']}")
    print(f"max_rps={cp['cover']['max_rps']} max_requests_per_hour={cp['cover']['max_requests_per_hour']}")
    p=Path(cp['cover']['status_file'])
    if not p.exists():
        print(f"runtime_status=unavailable ({p})")
        return
    try:
        obj=json.loads(p.read_text())
    except Exception as e:
        die(f"cannot read cover status: {e}")
    print(json.dumps(obj,indent=2))


def cover_test(args):
    cp=load_conf(); validate_config(cp)
    url=cp['server']['public_url'].rstrip('/')+cp['cover']['path']+'probe.json'
    p=run(['curl','-sS','-o','/tmp/mbox-cover-test','-w','%{http_code}','--max-time','10',url],check=False,capture=True)
    code=(p.stdout or '').strip()
    print(f"HTTP={code} URL={url}")
    if code != '200':
        die('cover endpoint is not reachable through the public frontend')
    print('cover route: OK')


def cover_simulate(args):
    cp=load_conf(); validate_config(cp); cv=cp['cover']
    seed=args.seed if args.seed is not None else secrets.randbits(64)
    rng=random.Random(seed)
    end=float(args.hours)*3600.0
    t=0.0; requests=0; states={"quiet":0,"casual":0,"active":0,"burst":0}; periods=0; max_period=0
    weights=[int(cv[k]) for k in ['quiet_weight','casual_weight','active_weight','burst_weight']]
    names=['quiet','casual','active','burst']
    cap=int(cv['max_requests_per_hour'])
    while t < end:
        t += rng.uniform(int(cv['start_grace_min_ms']),int(cv['start_grace_max_ms']))/1000.0
        if t>=end: break
        profile=rng.choices(names,weights=weights,k=1)[0]; states[profile]+=1; periods+=1; before=requests
        if profile=='quiet':
            t += rng.uniform(60,20*60)
        elif profile=='casual':
            for _ in range(rng.randint(1,5)):
                n=rng.randint(4,14); requests+=n; t+=n*rng.uniform(.12,.9); t+=rng.uniform(5,120)
        elif profile=='active':
            for _ in range(rng.randint(3,12)):
                n=rng.randint(8,30); requests+=n; t+=n*rng.uniform(.06,.5); t+=rng.uniform(2,45)
        else:
            n=rng.randint(30,120); requests+=n; t+=n*rng.uniform(.05,.35)
        max_period=max(max_period,requests-before)
        theoretical=int((t/3600.0+1)*cap)
        requests=min(requests,theoretical)
    print(f"seed={seed}")
    print(f"simulated_hours={args.hours}")
    print(f"estimated_requests={requests}")
    print(f"average_requests_per_hour={requests/max(float(args.hours),0.001):.1f}")
    print(f"activity_periods={periods} max_period_requests={max_period}")
    print('states='+ ' '.join(f'{k}={v}' for k,v in states.items()))
    print('No network requests were sent.')


def generate_relay_files():
    cp=load_conf(); validate_config(cp); mkdirs(); REGRU.mkdir(parents=True,exist_ok=True)
    if not RELAY_TEMPLATE.exists(): die(f"relay binary template not found: {RELAY_TEMPLATE}")
    shutil.copy2(RELAY_TEMPLATE, REGRU/"relay.fcgi"); os.chmod(REGRU/"relay.fcgi",0o755)
    base=cp["server"]["base_path"]
    obj={
        "target":cp["server"]["origin_url"],
        "allowed_exact":[base+"/health"],
        "allowed_prefixes":[base+"/api/v1/"],
        "max_slots":int(cp["relay"]["max_slots"]),
        "max_body_bytes":int(cp["relay"]["max_body_bytes"]),
        "slot_lock_prefix":cp["relay"]["slot_lock_prefix"],
    }
    (REGRU/"relay.json").write_text(json.dumps(obj,indent=2)+"\n"); os.chmod(REGRU/"relay.json",0o600)
    instructions=f"""MBox REG.RU deployment bundle\n\nAuthoritative generated files on RU server:\n  {REGRU}/relay.fcgi\n  {REGRU}/relay.json\n\nUpload both files into the web root used by the frontend.\nThe existing FastCGI routing/.htaccess is deployment-specific and is not overwritten automatically.\nPublic endpoint: {cp['server']['public_url']}\nOrigin target: {cp['server']['origin_url']}\nBase path: {base}\n"""
    (REGRU/"DEPLOY.txt").write_text(instructions)
    hashes=[]
    for n in ["relay.fcgi","relay.json"]:
        b=(REGRU/n).read_bytes(); hashes.append(f"{hashlib.sha256(b).hexdigest()}  {n}")
    (REGRU/"SHA256SUMS").write_text("\n".join(hashes)+"\n")
    return REGRU


def generate_origin_files():
    cp=load_conf(); validate_config(cp); ORIGIN.mkdir(parents=True,exist_ok=True)
    origin=urlparse(cp["server"]["origin_url"])
    host=origin.hostname
    base=cp["server"]["base_path"]
    listen=cp["server"]["listen"]
    text=f"""# Generated by mboxctl. Review before merging into an existing Caddyfile.
{host} {{
    @mbox_health path {base}/health
    respond @mbox_health "ok" 200

    @mbox path {base}/api/v1/*
    reverse_proxy @mbox {listen}
}}
"""
    (ORIGIN/"Caddyfile.mbox").write_text(text)
    (ORIGIN/"DEPLOY.txt").write_text(
        f"Generated origin reverse-proxy snippet: {ORIGIN}/Caddyfile.mbox\n"
        "Do not blindly overwrite an existing Caddyfile. Merge the host block or its matchers, then run caddy validate and reload.\n")
    return ORIGIN

def origin_paths(args):
    d=generate_origin_files(); print(f"Generated origin directory: {d}")
    for p in sorted(d.iterdir()): print(f"  {p}")

def origin_caddy(args):
    d=generate_origin_files(); print((d/"Caddyfile.mbox").read_text(),end="")

def relay_bundle(args):
    require_root(); d=generate_relay_files(); out=Path(args.output or (STATE/"regru"/"mbox-regru-bundle.tar.gz"))
    out.parent.mkdir(parents=True,exist_ok=True)
    with tarfile.open(out,"w:gz") as tf:
        for p in d.iterdir(): tf.add(p,arcname=p.name)
    os.chmod(out,0o600)
    print(out)


def relay_paths(args):
    generate_relay_files()
    print(f"Generated REG.RU directory: {REGRU}")
    for p in sorted(REGRU.iterdir()): print(f"  {p}")


def ftp_remote_dir_candidates(remote_dir, ftp_user):
    """Return likely FTP-visible paths for an SSH/hosting path.

    Shared-hosting FTP accounts are commonly chrooted, so an SSH path such as
    /var/www/USER/data/www/example.com is not necessarily visible verbatim via FTP.
    Keep the user supplied path first, then try safe chroot-relative forms.
    """
    raw=(remote_dir or "").strip()
    if not raw:
        return []
    raw=re.sub(r"/+","/",raw)
    if len(raw)>1:
        raw=raw.rstrip("/")
    out=[]
    def add(x):
        if not x:
            return
        x=re.sub(r"/+","/",x)
        if len(x)>1:
            x=x.rstrip("/")
        if x not in out:
            out.append(x)
    add(raw)
    # REG.RU/ISPmanager SSH layout: /var/www/<user>/data/www/<domain>
    prefix=f"/var/www/{ftp_user}/data"
    if raw == prefix:
        add("/")
    elif raw.startswith(prefix+"/"):
        rel=raw[len(prefix):]
        add(rel)                 # /www/example.com
        add(rel.lstrip("/"))    # www/example.com
        if rel.startswith("/www/"):
            site=rel[len("/www/"):]
            add("/"+site)       # some FTP accounts are rooted at www/
            add(site)
    # Generic fallback if the login differs from the SSH account name.
    m=re.match(r"^/var/www/[^/]+/data/(.+)$",raw)
    if m:
        rel="/"+m.group(1).lstrip("/")
        add(rel)
        add(rel.lstrip("/"))
        if rel.startswith("/www/"):
            site=rel[len("/www/"):]
            add("/"+site)
            add(site)
    return out


def ftp_cwd_smart(ftp, remote_dir, ftp_user):
    attempted=[]
    for candidate in ftp_remote_dir_candidates(remote_dir,ftp_user):
        try:
            ftp.cwd(candidate)
            return candidate
        except ftplib.all_errors as e:
            attempted.append((candidate,str(e)))
    try:
        pwd=ftp.pwd()
    except ftplib.all_errors:
        pwd="?"
    try:
        root=ftp.nlst()
        shown=", ".join(root[:20]) if root else "(empty)"
    except ftplib.all_errors as e:
        shown=f"unavailable: {e}"
    detail="; ".join(f"{path}: {err}" for path,err in attempted)
    die(
        "FTP directory not found. "
        f"Server PWD={pwd}; root entries={shown}. "
        f"Tried: {detail}. "
        "For REG.RU the SSH path /var/www/USER/data/www/DOMAIN is usually FTP-visible as /www/DOMAIN "
        "(or DOMAIN when the FTP account is chrooted to www)."
    )


def relay_deploy(args):
    require_root(); d=generate_relay_files(); files=[d/"relay.fcgi",d/"relay.json",d/"DEPLOY.txt",d/"SHA256SUMS"]
    if args.ssh:
        if not args.remote_dir: die("--remote-dir is required with --ssh")
        remote=args.remote_dir.rstrip("/")
        ts=dt.datetime.now().strftime("%Y%m%d-%H%M%S")
        stage=f"{remote}/.mbox-stage-{ts}"
        run(["ssh",args.ssh,"mkdir -p "+shlex.quote(stage)])
        run(["scp",str(d/"relay.fcgi"),str(d/"relay.json"),f"{args.ssh}:{stage}/"] )
        script=(
            f"set -e; cd {shlex.quote(remote)}; "
            f"[ ! -f relay.fcgi ] || cp -p relay.fcgi relay.fcgi.rollback-{ts}; "
            f"[ ! -f relay.json ] || cp -p relay.json relay.json.rollback-{ts}; "
            f"cp {shlex.quote(stage)}/relay.fcgi relay.fcgi.new; chmod 755 relay.fcgi.new; "
            f"cp {shlex.quote(stage)}/relay.json relay.json.new; chmod 600 relay.json.new; "
            f"mv -f relay.fcgi.new relay.fcgi; mv -f relay.json.new relay.json; "
            f"rm -rf {shlex.quote(stage)}"
        )
        run(["ssh",args.ssh,script])
        print(f"Uploaded atomically to {args.ssh}:{remote}/ (rollback copies kept)")
        return
    if args.ftp_host:
        if not args.ftp_user or not args.remote_dir: die("--ftp-user and --remote-dir are required")
        password=os.environ.get("MBOX_FTP_PASSWORD") or getpass.getpass("FTP password: ")
        cls=ftplib.FTP_TLS if not args.ftp_plain else ftplib.FTP
        ftp=cls(args.ftp_host,timeout=20); ftp.login(args.ftp_user,password)
        if isinstance(ftp,ftplib.FTP_TLS): ftp.prot_p()
        selected_dir=ftp_cwd_smart(ftp,args.remote_dir,args.ftp_user)
        print(f"FTP directory: {selected_dir} (PWD {ftp.pwd()})")
        ts=dt.datetime.now().strftime("%Y%m%d-%H%M%S")
        existing=set(ftp.nlst())
        for p in files[:2]:
            name=p.name
            if name in existing:
                try: ftp.rename(name,f"{name}.rollback-{ts}")
                except ftplib.all_errors: pass
            temp=name+".new"
            with p.open("rb") as f: ftp.storbinary(f"STOR {temp}",f)
            try: ftp.rename(temp,name)
            except ftplib.all_errors:
                try: ftp.delete(name)
                except ftplib.all_errors: pass
                ftp.rename(temp,name)
            if name == "relay.fcgi":
                try: ftp.sendcmd("SITE CHMOD 755 relay.fcgi")
                except ftplib.all_errors: pass
            print(f"uploaded {name}")
        ftp.quit(); return
    die("choose --ssh USER@HOST or --ftp-host HOST")


def relay_test(args):
    cp=load_conf(); url=cp["server"]["public_url"]+cp["camouflage"]["open_path"]
    p=run(["curl","-sS","-o","/dev/null","-w","%{http_code}\n","-X","POST",url,"--data-binary","{}","--max-time","10"],check=False,capture=True)
    print((p.stdout or "").strip())
    print("Expected for unauthenticated probe: normally 404; reachability is what this check verifies.")


def doctor(args):
    cp=load_conf(); validate_config(cp); print("config: OK")
    con=db_connect(); ok=con.execute("PRAGMA integrity_check").fetchone()[0]; n=con.execute("SELECT count(*) FROM users WHERE enabled=1").fetchone()[0]; con.close()
    print(f"sqlite: {ok}; enabled_users={n}")
    for p in [cp["server"]["server_private_key"],cp["server"]["server_public_key"],str(RUNTIME),str(RELAY_TEMPLATE)]:
        print(f"{p}: {'OK' if Path(p).exists() else 'MISSING'}")
    print(f"service: {'active' if service_active() else 'inactive'}")
    print(f"REG.RU files: {REGRU}")


def backup_create(args):
    require_root(); out=Path(args.output or f"/root/mbox-backup-{dt.datetime.now().strftime('%Y%m%d-%H%M%S')}.tar.gz")
    out.parent.mkdir(parents=True,exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="mbox-backup-") as td:
        dbcopy=Path(td)/"users.db"
        src=db_connect(); dst=sqlite3.connect(dbcopy); src.backup(dst); dst.close(); src.close()
        with tarfile.open(out,"w:gz") as tf:
            if ETC.exists(): tf.add(ETC,arcname="etc-mbox")
            tf.add(dbcopy,arcname="users.db")
    os.chmod(out,0o600); print(out)


def status(args):
    service_action("status")
    if CONF.exists():
        cp=load_conf(); print(f"Admin config: {CONF}"); print(f"Runtime config: {RUNTIME}"); print(f"Users DB: {DB}"); print(f"REG.RU: {REGRU}")


def make_parser():
    p=argparse.ArgumentParser(prog="mboxctl",description="MBox administration CLI")
    sp=p.add_subparsers(dest="cmd",required=True)
    q=sp.add_parser("init"); q.add_argument("--force",action="store_true",help="destructive reinitialize: backup old MBox state, then create fresh config/keys/users"); q.add_argument("--public-url"); q.add_argument("--origin-url"); q.add_argument("--base-path"); q.add_argument("--first-user"); q.add_argument("--listen",default="127.0.0.1:18449"); q.add_argument("--upstream",default="127.0.0.1:2080"); q.add_argument("--max-slots",type=int,default=4); q.add_argument("--max-body-bytes",type=int,default=2097152); q.add_argument("--slot-lock-prefix",default="/tmp/mbox-relay"); q.set_defaults(func=init_cmd)
    m=sp.add_parser("migrate"); ms=m.add_subparsers(dest="sub",required=True); q=ms.add_parser("legacy"); q.add_argument("--server-json",default="/etc/mbox/server.json"); q.add_argument("--public-url"); q.add_argument("--origin-url"); q.set_defaults(func=migrate_legacy)
    u=sp.add_parser("user"); us=u.add_subparsers(dest="sub",required=True)
    q=us.add_parser("add"); q.add_argument("ids",nargs="*"); q.add_argument("--count",type=int); q.add_argument("--prefix",default="user-"); q.add_argument("--width",type=int); q.add_argument("--note"); q.set_defaults(func=user_add)
    q=us.add_parser("import"); q.add_argument("csv_file"); q.set_defaults(func=user_import)
    q=us.add_parser("list"); q.add_argument("--enabled",action="store_true"); q.add_argument("--revoked",action="store_true"); q.set_defaults(func=user_list)
    q=us.add_parser("show"); q.add_argument("id"); q.set_defaults(func=user_show)
    q=us.add_parser("count"); q.set_defaults(func=user_count)
    q=us.add_parser("rename"); q.add_argument("id"); q.add_argument("new_id"); q.set_defaults(func=user_rename)
    for name in ["enable","disable","revoke"]:
        q=us.add_parser(name); q.add_argument("id"); q.set_defaults(func=lambda a,n=name:user_state(a,n))
    q=us.add_parser("rotate"); q.add_argument("id"); q.add_argument("--print-link",action="store_true"); q.add_argument("--name"); q.set_defaults(func=user_rotate)
    q=us.add_parser("delete"); q.add_argument("ids",nargs="+"); q.add_argument("-y","--yes",action="store_true",help="delete without interactive confirmation"); q.add_argument("--purge",action="store_true",help=argparse.SUPPRESS); q.set_defaults(func=user_delete)
    q=us.add_parser("link"); q.add_argument("id"); q.add_argument("--name"); q.set_defaults(func=user_link)
    q=us.add_parser("qr"); q.add_argument("id"); q.add_argument("--name"); q.set_defaults(func=user_qr)
    q=us.add_parser("export"); q.add_argument("ids",nargs="*"); q.add_argument("--all-enabled",action="store_true"); q.add_argument("--output",required=True); q.add_argument("--no-qr",action="store_true"); q.set_defaults(func=user_export)
    c=sp.add_parser("config"); cs=c.add_subparsers(dest="sub",required=True)
    q=cs.add_parser("show"); q.set_defaults(func=config_show)
    q=cs.add_parser("upgrade"); q.set_defaults(func=config_upgrade)
    q=cs.add_parser("get"); q.add_argument("key"); q.set_defaults(func=config_get)
    q=cs.add_parser("set"); q.add_argument("key"); q.add_argument("value"); q.set_defaults(func=config_set)
    q=cs.add_parser("validate"); q.set_defaults(func=lambda a: (validate_config(load_conf()),print("OK")))
    cv=sp.add_parser("cover"); cvs=cv.add_subparsers(dest="sub",required=True)
    q=cvs.add_parser("status"); q.set_defaults(func=cover_status)
    q=cvs.add_parser("enable"); q.set_defaults(func=lambda a: cover_set_enabled(True))
    q=cvs.add_parser("disable"); q.set_defaults(func=lambda a: cover_set_enabled(False))
    q=cvs.add_parser("test"); q.set_defaults(func=cover_test)
    q=cvs.add_parser("simulate"); q.add_argument("--hours",type=float,default=24.0); q.add_argument("--seed",type=int); q.set_defaults(func=cover_simulate)
    o=sp.add_parser("origin"); osub=o.add_subparsers(dest="sub",required=True)
    q=osub.add_parser("paths"); q.set_defaults(func=origin_paths)
    q=osub.add_parser("caddy"); q.set_defaults(func=origin_caddy)
    r=sp.add_parser("relay"); rs=r.add_subparsers(dest="sub",required=True)
    q=rs.add_parser("bundle"); q.add_argument("--output"); q.set_defaults(func=relay_bundle)
    q=rs.add_parser("paths"); q.set_defaults(func=relay_paths)
    q=rs.add_parser("deploy"); q.add_argument("--ssh"); q.add_argument("--ftp-host"); q.add_argument("--ftp-user"); q.add_argument("--ftp-plain",action="store_true"); q.add_argument("--remote-dir"); q.set_defaults(func=relay_deploy)
    q=rs.add_parser("test"); q.set_defaults(func=relay_test)
    s=sp.add_parser("server"); ss=s.add_subparsers(dest="sub",required=True)
    for name in ["start","stop","restart","status"]:
        q=ss.add_parser(name); q.set_defaults(func=lambda a,n=name: service_action(n))
    q=sp.add_parser("sync"); q.add_argument("--restart",action="store_true"); q.set_defaults(func=lambda a: render_runtime(reload_auth=not a.restart,restart=a.restart))
    q=sp.add_parser("doctor"); q.set_defaults(func=doctor)
    q=sp.add_parser("status"); q.set_defaults(func=status)
    q=sp.add_parser("logs"); q.add_argument("-f","--follow",action="store_true"); q.add_argument("-n","--lines",type=int,default=100); q.set_defaults(func=logs_cmd)
    d=sp.add_parser("db"); ds=d.add_subparsers(dest="sub",required=True); q=ds.add_parser("check"); q.set_defaults(func=db_check); q=ds.add_parser("vacuum"); q.set_defaults(func=db_vacuum)
    l=sp.add_parser("link"); ls=l.add_subparsers(dest="sub",required=True); q=ls.add_parser("inspect"); q.add_argument("path"); q.add_argument("--redact",action="store_true"); q.set_defaults(func=link_inspect)
    b=sp.add_parser("backup"); bs=b.add_subparsers(dest="sub",required=True); q=bs.add_parser("create"); q.add_argument("--output"); q.set_defaults(func=backup_create); q=bs.add_parser("verify"); q.add_argument("path"); q.set_defaults(func=backup_verify)
    q=sp.add_parser("version"); q.set_defaults(func=lambda a: print("mboxctl 1.0.0-beta.1"))
    return p


def main():
    args=make_parser().parse_args(); rv=args.func(args); return 0 if rv is None else (rv if isinstance(rv,int) else 0)

if __name__=="__main__":
    raise SystemExit(main())
