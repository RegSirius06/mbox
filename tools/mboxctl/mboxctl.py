#!/usr/bin/env python3
import argparse
import base64
import configparser
import csv
import datetime as dt
import ftplib
import getpass
import grp
import hashlib
import json
import os
import re
import random
import time
import secrets
import shlex
import shutil
import socket
import sqlite3
import stat
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path
from urllib.parse import quote, urlparse

ETC = Path(os.environ.get("MBOX_ETC", "/etc/mbox"))
STATE = Path(os.environ.get("MBOX_STATE", "/var/lib/mbox"))
CONF = ETC / "mbox.conf"
RUNTIME = ETC / "server.json"
DB = STATE / "users.db"
REGRU = STATE / "regru" / "current"
ORIGIN = STATE / "origin" / "current"
RELAY_TEMPLATE = Path(os.environ.get("MBOX_RELAY_TEMPLATE", "/usr/local/lib/mbox/relay.fcgi"))
SITE_TEMPLATE = Path(os.environ.get("MBOX_SITE_TEMPLATE", "/usr/local/share/mbox/regru-site"))
SERVICE = "mbox-server.service"
RUNTIME_STATUS = Path(os.environ.get("MBOX_RUNTIME_STATUS", "/run/mbox/status.json"))
KEY_TOOL = Path(os.environ.get("MBOX_KEY_TOOL", "/usr/local/bin/mbox-key"))
ORIGIN_HMAC_KEY = ETC / "origin-hmac.key"
CREDENTIAL_DIR = ETC / "credentials.d"
CREDENTIAL_SOCKET = Path(os.environ.get("MBOX_CREDENTIAL_SOCKET", "/run/mbox/credentials.sock"))
CLIENT_ID_RE = re.compile(r"^[A-Za-z0-9._-]{1,63}$")

DEFAULT_CAMO = {
    "open_path": "/api/v1/session",
    "up_path": "/api/v1/events/",
    "down_path": "/api/v1/sync/",
    "close_path": "/api/v1/session/",
    "content_type": "application/json",
    "down_max_bytes": "262144",
    "up_max_bytes": "262144",
    "padding_classes": "512,1024,2048,4096,8192,16384,32768,65536,131072,262144,524288",
    "padding_extra_class_percent": "0",
    "padding_random_max_bytes": "16384",
    "batch_min_bytes": "73728",
    "noise_chunk_min_bytes": "16384",
    "noise_chunk_max_bytes": "61440",
    "idle_min_ms": "45000",
    "idle_max_ms": "90000",
    "wake_min_ms": "100",
    "wake_max_ms": "220",
    "active_jitter_percent": "35",
    "active_min_ms": "120",
    "active_max_ms": "420",
    "active_backoff_max_ms": "2200",
    "failure_timeout_ms": "30000",
    "retry_min_ms": "300",
    "retry_max_ms": "5000",
}


def eprint(msg):
    print(msg, file=sys.stderr)

DEFAULT_LIMITS = {
    # Conservative defaults for cheap 1 GiB / 1 vCPU VPSes.  Queued memory is
    # deliberately bounded far below total RAM; raise these only after load tests.
    "max_sessions_global": "128",
    "default_max_sessions_per_client": "4",
    "max_streams_per_session": "128",
    "max_total_streams": "1024",
    "max_stream_opens_per_second": "32",
    "max_session_opens_per_minute": "180",
    "max_concurrent_handshakes": "32",
    "max_handshake_attempts_per_second": "64",
    "max_down_queue_bytes": "4194304",
    "max_down_replay_bytes": "8388608",
    "global_queued_bytes": "67108864",
}

DEFAULT_AUTH = {
    # Transitional compatibility: new profiles use opaque random auth handles.
    # Set false after all legacy profiles have migrated.
    "accept_legacy_client_ids": "true",
    # Leave permissive until all deployed clients send session-scoped cmac.
    # Strict mode rejects unsigned DOWN/UP/CLOSE HTTP controls.
    "require_authenticated_control": "false",
}


DEFAULT_COVER = {
    "enabled": "false",
    "path": "/api/v1/assets/",
    "status_file": "/run/mbox/cover-status.json",
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
    try:
        gid = grp.getgrnam("mbox").gr_gid
        os.chown(ETC, 0, gid)
        os.chmod(ETC, 0o750)
    except KeyError:
        os.chmod(ETC, 0o700)
    os.chmod(STATE, 0o700)


def load_conf(required=True):
    cp = configparser.ConfigParser(interpolation=None)
    if not CONF.exists():
        if required:
            die(f"configuration not found: {CONF}; run 'mboxctl init'")
        return cp
    cp.read(CONF)
    for section, defaults in (("camouflage", DEFAULT_CAMO), ("cover", DEFAULT_COVER), ("limits", DEFAULT_LIMITS), ("auth", DEFAULT_AUTH)):
        if section not in cp:
            cp[section] = defaults.copy()
        else:
            for k, v in defaults.items():
                if k not in cp[section]:
                    cp[section][k] = v
    if "server" in cp and not cp["server"].get("origin_hmac_secret", "").strip():
        cp["server"]["origin_hmac_secret"] = str(ORIGIN_HMAC_KEY)
    if "server" in cp and not cp["server"].get("credential_dir", "").strip():
        cp["server"]["credential_dir"] = str(CREDENTIAL_DIR)
    if "server" in cp and not cp["server"].get("credential_reload_socket", "").strip():
        cp["server"]["credential_reload_socket"] = str(CREDENTIAL_SOCKET)
    if "relay" in cp and not cp["relay"].get("runtime_dir", "").strip():
        # Persisted configs should be upgraded with `mboxctl config upgrade`;
        # do not silently reuse the old predictable slot_lock_prefix.
        cp["relay"]["runtime_dir"] = "/tmp/mbox-relay-" + secrets.token_hex(12)
    return cp


def apply_server_read_permissions(cp=None):
    try:
        gid=grp.getgrnam("mbox").gr_gid
    except KeyError:
        return
    try:
        os.chown(ETC,0,gid); os.chmod(ETC,0o750)
    except FileNotFoundError:
        return
    paths=[RUNTIME]
    if cp is not None and "server" in cp:
        for key in ("server_private_key","origin_hmac_secret"):
            value=cp["server"].get(key,"").strip()
            if value: paths.append(Path(value))
    for path in paths:
        try: os.chown(path,0,gid); os.chmod(path,0o640)
        except FileNotFoundError: pass


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
      auth_handle BLOB CHECK(auth_handle IS NULL OR length(auth_handle)=16),
      psk BLOB NOT NULL CHECK(length(psk)=32),
      enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
      label TEXT NOT NULL DEFAULT '',
      note TEXT NOT NULL DEFAULT '',
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL,
      rotated_at TEXT,
      revoked_at TEXT,
      max_sessions INTEGER NOT NULL DEFAULT 0,
      expires_at TEXT
    );
    CREATE INDEX IF NOT EXISTS idx_users_enabled ON users(enabled);
    """)
    # Online schema migration for databases created by beta.1/beta.2.
    cols={row[1] for row in con.execute("PRAGMA table_info(users)")}
    if "max_sessions" not in cols:
        con.execute("ALTER TABLE users ADD COLUMN max_sessions INTEGER NOT NULL DEFAULT 0")
    if "expires_at" not in cols:
        con.execute("ALTER TABLE users ADD COLUMN expires_at TEXT")
    if "auth_handle" not in cols:
        con.execute("ALTER TABLE users ADD COLUMN auth_handle BLOB")
    # Opaque 128-bit wire identities prevent human-readable account names from
    # appearing in pre-Noise OPEN metadata for newly provisioned clients.
    rows=con.execute("SELECT id FROM users WHERE auth_handle IS NULL OR length(auth_handle) != 16").fetchall()
    for row in rows:
        while True:
            candidate=secrets.token_bytes(16)
            if con.execute("SELECT 1 FROM users WHERE auth_handle=?",(candidate,)).fetchone() is None:
                con.execute("UPDATE users SET auth_handle=? WHERE id=?",(candidate,row["id"]))
                break
    con.execute("CREATE UNIQUE INDEX IF NOT EXISTS idx_users_auth_handle ON users(auth_handle)")
    con.commit()
    try:
        os.chmod(DB, 0o600)
    except FileNotFoundError:
        pass
    return con


def allocate_auth_handle(con):
    """Allocate a collision-resistant opaque wire identity inside the DB transaction."""
    for _ in range(128):
        candidate=secrets.token_bytes(16)
        if con.execute("SELECT 1 FROM users WHERE auth_handle=?",(candidate,)).fetchone() is None:
            return candidate
    die("failed to allocate a unique auth handle")


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
    apply_server_read_permissions(cp)


def ensure_origin_hmac_key(cp):
    path=Path(cp["server"].get("origin_hmac_secret", str(ORIGIN_HMAC_KEY)))
    path.parent.mkdir(parents=True,exist_ok=True)
    if not path.exists():
        path.write_text(secrets.token_hex(32)+"\n")
    raw=path.read_text().strip()
    try: key=bytes.fromhex(raw)
    except ValueError: die(f"{path}: invalid origin HMAC key")
    if len(key)!=32: die(f"{path}: origin HMAC key must be 32 bytes")
    os.chmod(path,0o600)
    apply_server_read_permissions(cp)
    return path


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
        "up_max_bytes": int(c["up_max_bytes"]),
        "padding_classes": [int(x) for x in c["padding_classes"].split(",") if x.strip()],
        "padding_extra_class_percent": int(c["padding_extra_class_percent"]),
        "padding_random_max_bytes": int(c["padding_random_max_bytes"]),
        "batch_min_bytes": int(c["batch_min_bytes"]),
        "noise_chunk_min_bytes": int(c["noise_chunk_min_bytes"]),
        "noise_chunk_max_bytes": int(c["noise_chunk_max_bytes"]),
    }
    if include_timing:
        d["timing"] = {
            "idle_min_ms": int(c["idle_min_ms"]),
            "idle_max_ms": int(c["idle_max_ms"]),
            "wake_min_ms": int(c["wake_min_ms"]),
            "wake_max_ms": int(c["wake_max_ms"]),
            "active_jitter_percent": int(c["active_jitter_percent"]),
            "active_min_ms": int(c["active_min_ms"]),
            "active_max_ms": int(c["active_max_ms"]),
            "active_backoff_max_ms": int(c["active_backoff_max_ms"]),
        }
        d["recovery"] = {
            "failure_timeout_ms": int(c["failure_timeout_ms"]),
            "retry_min_ms": int(c["retry_min_ms"]),
            "retry_max_ms": int(c["retry_max_ms"]),
        }
    return d



def ensure_credential_dir(cp):
    path=Path(cp["server"].get("credential_dir", str(CREDENTIAL_DIR)))
    path.mkdir(parents=True,exist_ok=True)
    try:
        gid=grp.getgrnam("mbox").gr_gid
        os.chown(path,0,gid)
        os.chmod(path,0o750)
    except KeyError:
        os.chmod(path,0o700)
    return path


def credential_object(row, cp):
    obj={
        "auth_handle":bytes(row["auth_handle"]).hex(),
        "internal_id":row["id"],
        "psk":bytes(row["psk"]).hex(),
        "max_sessions":int(row["max_sessions"] or 0),
    }
    if cp.getboolean("auth","accept_legacy_client_ids"):
        obj["legacy_id"]=row["id"]
    if row["expires_at"]:
        obj["expires_at"]=row["expires_at"]
    return obj


def write_credential_row(row, cp):
    directory=ensure_credential_dir(cp)
    handle=bytes(row["auth_handle"]).hex()
    path=directory/f"{handle}.json"
    tmp=directory/f".{handle}.{os.getpid()}.tmp"
    tmp.write_text(json.dumps(credential_object(row,cp),separators=(",",":"))+"\n")
    os.chmod(tmp,0o600)
    try:
        gid=grp.getgrnam("mbox").gr_gid
        os.chown(tmp,0,gid); os.chmod(tmp,0o640)
    except KeyError:
        pass
    os.replace(tmp,path)
    return handle,path


def notify_credential_change(cp, op, handle):
    if not service_active():
        return
    sock_path=cp["server"].get("credential_reload_socket",str(CREDENTIAL_SOCKET))
    payload=(json.dumps({"op":op,"auth_handle":handle},separators=(",",":"))+"\n").encode()
    try:
        with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as sock:
            sock.settimeout(2.0); sock.connect(sock_path); sock.sendall(payload)
            response=sock.recv(64)
        if response != b"ok\n":
            raise RuntimeError("credential admin rejected update")
    except (OSError,RuntimeError) as exc:
        # Safe fallback for upgrades: SIGHUP reloads the credential directory in
        # one operation. It does not require rewriting the monolithic JSON.
        eprint(f"warning: incremental credential reload failed ({exc}); falling back to SIGHUP")
        service_hup()


def sync_user_credential(uid, notify=True):
    cp=load_conf(); con=db_connect()
    row=con.execute("SELECT * FROM users WHERE id=?",(uid,)).fetchone(); con.close()
    if not row:
        die(f"user not found: {uid}")
    handle=bytes(row["auth_handle"]).hex(); directory=ensure_credential_dir(cp)
    path=directory/f"{handle}.json"
    if row["enabled"]:
        write_credential_row(row,cp)
        if notify: notify_credential_change(cp,"reload",handle)
    else:
        try: path.unlink()
        except FileNotFoundError: pass
        if notify: notify_credential_change(cp,"delete",handle)
    return handle


def remove_credential_handle(handle, cp=None, notify=True):
    cp=cp or load_conf(); directory=ensure_credential_dir(cp)
    path=directory/f"{handle}.json"
    try: path.unlink()
    except FileNotFoundError: pass
    if notify: notify_credential_change(cp,"delete",handle)


def sync_all_credentials(cp=None):
    cp=cp or load_conf(); directory=ensure_credential_dir(cp); con=db_connect()
    rows=con.execute("SELECT * FROM users ORDER BY id").fetchall(); con.close()
    expected=set()
    for row in rows:
        handle=bytes(row["auth_handle"]).hex()
        if row["enabled"]:
            expected.add(handle+".json"); write_credential_row(row,cp)
    for path in directory.glob("*.json"):
        if path.name not in expected:
            path.unlink()


def render_runtime(reload_auth=False, restart=False):
    cp = load_conf()
    validate_config(cp)
    sync_all_credentials(cp)
    clients = {}
    obj = {
        "camouflage": camo_dict(cp, include_timing=False),
        "transport": {
            "listen": cp["server"]["listen"],
            "upstream": cp["server"]["upstream"],
            "base_path": cp["server"]["base_path"],
            "origin_hmac_secret_file": cp["server"]["origin_hmac_secret"],
        },
        "crypto": {
            "server_private_key_file": cp["server"]["server_private_key"],
            "credential_dir": cp["server"]["credential_dir"],
            "credential_reload_socket": cp["server"]["credential_reload_socket"],
            "require_authenticated_control": cp.getboolean("auth", "require_authenticated_control"),
            "clients": clients,
        },
        "limits": {k:int(cp["limits"][k]) for k in DEFAULT_LIMITS},
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
    apply_server_read_permissions(cp)
    if restart:
        service_action("restart", quiet=True)
    elif reload_auth:
        service_hup()


def service_active():
    p = subprocess.run(["systemctl", "is-active", "--quiet", SERVICE], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
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
    needed = [("server", k) for k in ["listen", "upstream", "base_path", "public_url", "origin_url", "server_private_key", "server_public_key", "origin_hmac_secret", "credential_dir", "credential_reload_socket"]]
    needed += [("relay", k) for k in ["max_slots", "max_body_bytes", "runtime_dir"]]
    needed += [("camouflage", k) for k in DEFAULT_CAMO]
    needed += [("cover", k) for k in DEFAULT_COVER]
    needed += [("limits", k) for k in DEFAULT_LIMITS]
    needed += [("auth", k) for k in DEFAULT_AUTH]
    for section, key in needed:
        if section not in cp or not cp[section].get(key, "").strip():
            die(f"missing config: {section}.{key}")
    try:
        cp.getboolean("auth", "require_authenticated_control")
    except ValueError:
        die("auth.require_authenticated_control must be true or false")
    credential_dir = Path(cp["server"]["credential_dir"])
    credential_socket = Path(cp["server"]["credential_reload_socket"])
    if not credential_dir.is_absolute():
        die("server.credential_dir must be an absolute path")
    if not credential_socket.is_absolute():
        die("server.credential_reload_socket must be an absolute path")
    if credential_dir.exists():
        st = credential_dir.lstat()
        if not credential_dir.is_dir() or credential_dir.is_symlink():
            die("server.credential_dir must be a real directory, not a symlink")
        if st.st_mode & 0o007:
            die("server.credential_dir must not be accessible by other users")
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
    if not (20 <= int(cp["camouflage"]["active_min_ms"]) <= int(cp["camouflage"]["active_max_ms"]) <= 5000):
        die("camouflage active polling range invalid")
    if not (int(cp["camouflage"]["active_max_ms"]) <= int(cp["camouflage"]["active_backoff_max_ms"]) <= 30000):
        die("camouflage active_backoff_max_ms invalid")
    c=cp["camouflage"]
    pad_random=int(c["padding_random_max_bytes"]); batch_min=int(c["batch_min_bytes"])
    chunk_min=int(c["noise_chunk_min_bytes"]); chunk_max=int(c["noise_chunk_max_bytes"])
    up_max=int(c["up_max_bytes"]); down_max=int(c["down_max_bytes"])
    if not (0 <= pad_random <= 256*1024): die("camouflage.padding_random_max_bytes out of range")
    if not (65545 <= batch_min <= min(up_max,down_max)): die("camouflage.batch_min_bytes out of range")
    if not (1024 <= chunk_min <= chunk_max <= 65519): die("camouflage Noise chunk range invalid")
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
    try:
        cp.getboolean("auth","accept_legacy_client_ids")
    except ValueError:
        die("auth.accept_legacy_client_ids must be true or false")
    lm=cp["limits"]
    max_sessions=int(lm["max_sessions_global"]); default_sessions=int(lm["default_max_sessions_per_client"])
    streams_per_session=int(lm["max_streams_per_session"]); total_streams=int(lm["max_total_streams"])
    stream_rate=int(lm["max_stream_opens_per_second"]); session_rate=int(lm["max_session_opens_per_minute"])
    handshake_conc=int(lm["max_concurrent_handshakes"]); handshake_rate=int(lm["max_handshake_attempts_per_second"])
    down_queue=int(lm["max_down_queue_bytes"]); down_replay=int(lm["max_down_replay_bytes"]); global_queue=int(lm["global_queued_bytes"])
    if not (1 <= max_sessions <= 100000): die("limits.max_sessions_global out of range")
    if default_sessions != -1 and not (1 <= default_sessions <= 10000): die("limits.default_max_sessions_per_client must be -1 or 1..10000")
    if default_sessions > 0 and default_sessions > max_sessions: die("limits.default_max_sessions_per_client must be <= max_sessions_global")
    if not (1 <= streams_per_session <= 4096): die("limits.max_streams_per_session out of range")
    if not (streams_per_session <= total_streams <= 200000): die("limits.max_total_streams out of range")
    if not (1 <= stream_rate <= 10000): die("limits.max_stream_opens_per_second out of range")
    if not (1 <= session_rate <= 100000): die("limits.max_session_opens_per_minute out of range")
    if not (1 <= handshake_conc <= 4096): die("limits.max_concurrent_handshakes out of range")
    if not (1 <= handshake_rate <= 100000): die("limits.max_handshake_attempts_per_second out of range")
    if not (256*1024 <= down_queue <= 256*1024*1024): die("limits.max_down_queue_bytes out of range")
    if not (256*1024 <= down_replay <= 256*1024*1024): die("limits.max_down_replay_bytes out of range")
    if not (8*1024*1024 <= global_queue <= 2*1024*1024*1024): die("limits.global_queued_bytes out of range")
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
    origin = args.origin_url or prompt("MBox origin URL, e.g. https://origin.example.com")
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
        "origin_hmac_secret": str(ORIGIN_HMAC_KEY),
        "credential_dir": str(CREDENTIAL_DIR),
        "credential_reload_socket": str(CREDENTIAL_SOCKET),
    }
    cp["relay"] = {
        "max_slots": str(args.max_slots),
        "max_body_bytes": str(args.max_body_bytes),
        "runtime_dir": args.relay_runtime_dir or ("/tmp/mbox-relay-" + secrets.token_hex(12)),
    }
    cp["camouflage"] = DEFAULT_CAMO.copy()
    cp["cover"] = DEFAULT_COVER.copy()
    cp["limits"] = DEFAULT_LIMITS.copy()
    cp["auth"] = DEFAULT_AUTH.copy()
    try:
        save_conf(cp); ensure_keys(cp); ensure_origin_hmac_key(cp); db_connect().close()
        con = db_connect()
        con.execute("INSERT INTO users(id,auth_handle,psk,enabled,label,note,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)",
                    (first, allocate_auth_handle(con), secrets.token_bytes(32), 1, first, "initial user", now(), now()))
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
        "origin_hmac_secret": str(ORIGIN_HMAC_KEY),
        "credential_dir": str(CREDENTIAL_DIR),
        "credential_reload_socket": str(CREDENTIAL_SOCKET),
    }
    relay_obj = {}
    rp = Path("/root/mbox-production-out/relay.json")
    if rp.exists():
        try: relay_obj = json.loads(rp.read_text())
        except Exception: relay_obj = {}
    legacy_runtime = str(relay_obj.get("runtime_dir", "")).strip()
    if not legacy_runtime or not os.path.isabs(legacy_runtime):
        legacy_runtime = "/tmp/mbox-relay-" + secrets.token_hex(12)
    cp["relay"] = {
        "max_slots": str(relay_obj.get("max_slots", 4)),
        "max_body_bytes": str(relay_obj.get("max_body_bytes", 2097152)),
        "runtime_dir": legacy_runtime,
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
    cp["limits"] = DEFAULT_LIMITS.copy()
    cp["auth"] = DEFAULT_AUTH.copy()
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
            existing=con.execute("SELECT auth_handle FROM users WHERE id=?",(uid,)).fetchone()
            handle=existing["auth_handle"] if existing and existing["auth_handle"] is not None else allocate_auth_handle(con)
            con.execute("INSERT OR REPLACE INTO users(id,auth_handle,psk,enabled,label,note,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)",
                        (uid, handle, secret, 1, uid, "migrated from legacy config", now(), now()))
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


def normalize_expiry(value):
    if not value:
        return None
    text=value.strip()
    if text.lower() in {"none","never","unlimited"}:
        return None
    try:
        parsed=dt.datetime.fromisoformat(text.replace("Z","+00:00"))
    except ValueError:
        die("expiry must be RFC3339, e.g. 2026-12-01T00:00:00Z")
    if parsed.tzinfo is None:
        parsed=parsed.replace(tzinfo=dt.timezone.utc)
    return parsed.astimezone(dt.timezone.utc).replace(microsecond=0).isoformat()


def expiry_from_args(args):
    if getattr(args,"days",None) is not None:
        if args.days <= 0: die("--days must be > 0")
        return (dt.datetime.now(dt.timezone.utc)+dt.timedelta(days=args.days)).replace(microsecond=0).isoformat()
    if getattr(args,"expires_at",None):
        return normalize_expiry(args.expires_at)
    return None


def resolve_max_sessions(args):
    if getattr(args,"unlimited",False): return -1
    n=getattr(args,"max_sessions",None)
    if n is None: return 0
    if n < 1 or n > 10000: die("--max-sessions must be 1..10000")
    return n


def user_add(args):
    require_root(); load_conf()
    ids = list(args.ids)
    if args.count:
        width = args.width or max(3, len(str(args.count)))
        ids += [f"{args.prefix}{i:0{width}d}" for i in range(1, args.count+1)]
    if not ids: die("provide user ids or --count")
    for uid in ids: validate_client_id(uid)
    if len(ids) != len(set(ids)): die("duplicate user ids in request")
    con = db_connect(); ts=now(); max_sessions=resolve_max_sessions(args); expires_at=expiry_from_args(args)
    try:
        with con:
            for uid in ids:
                con.execute("INSERT INTO users(id,auth_handle,psk,enabled,label,note,created_at,updated_at,max_sessions,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
                            (uid, allocate_auth_handle(con), secrets.token_bytes(32), 1, uid, args.note or "", ts, ts, max_sessions, expires_at))
    except sqlite3.IntegrityError as e:
        die(f"user already exists: {e}")
    finally: con.close()
    for uid in ids: sync_user_credential(uid)
    for uid in ids: print(uid)
    print(f"Created {len(ids)} user(s).")


def user_import(args):
    require_root(); load_conf(); rows=[]
    with open(args.csv_file, newline="") as f:
        r=csv.DictReader(f)
        if not r.fieldnames or "id" not in r.fieldnames: die("CSV must contain an 'id' column")
        for row in r:
            uid=(row.get("id") or "").strip(); validate_client_id(uid)
            raw_limit=(row.get("max_sessions") or "").strip().lower()
            if raw_limit in {"", "default"}: max_sessions=0
            elif raw_limit in {"unlimited", "none", "-1"}: max_sessions=-1
            else:
                try: max_sessions=int(raw_limit)
                except ValueError: die(f"invalid max_sessions for {uid}")
                if max_sessions < 1 or max_sessions > 10000: die(f"max_sessions out of range for {uid}")
            expires_at=normalize_expiry((row.get("expires_at") or "").strip())
            rows.append((uid,(row.get("label") or uid).strip(),(row.get("note") or "").strip(),max_sessions,expires_at))
    con=db_connect(); ts=now()
    try:
        with con:
            for uid,label,note,max_sessions,expires_at in rows:
                con.execute("INSERT INTO users(id,auth_handle,psk,enabled,label,note,created_at,updated_at,max_sessions,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?)",
                            (uid,allocate_auth_handle(con),secrets.token_bytes(32),1,label,note,ts,ts,max_sessions,expires_at))
    except sqlite3.IntegrityError as e: die(str(e))
    finally: con.close()
    for row in rows: sync_user_credential(row[0])
    print(f"Imported {len(rows)} user(s).")


def read_runtime_status():
    try:
        obj=json.loads(RUNTIME_STATUS.read_text())
        if not isinstance(obj,dict): return {}
        return obj
    except (OSError,json.JSONDecodeError):
        return {}


def status_client_key(uid):
    return "client#"+hashlib.sha256(uid.encode()).digest()[:6].hex()


def user_list(args):
    con=db_connect()
    q="SELECT id,enabled,label,created_at,rotated_at,revoked_at,max_sessions,expires_at FROM users"
    cond=[]
    if args.enabled: cond.append("enabled=1")
    if args.revoked: cond.append("revoked_at IS NOT NULL")
    if cond: q += " WHERE "+" AND ".join(cond)
    q += " ORDER BY id"
    rows=con.execute(q).fetchall(); con.close()
    status=read_runtime_status(); active=status.get("clients",{}) if isinstance(status.get("clients",{}),dict) else {}
    print(f"{'ID':<24} {'STATE':<9} {'ACTIVE':>6} {'LIMIT':<10} {'EXPIRES':<22} LABEL")
    now_utc=dt.datetime.now(dt.timezone.utc)
    for r in rows:
        expired=False
        if r["expires_at"]:
            try: expired=dt.datetime.fromisoformat(r["expires_at"].replace("Z","+00:00")) <= now_utc
            except ValueError: expired=True
        state=("expired" if r["enabled"] and expired else ("enabled" if r["enabled"] else ("revoked" if r["revoked_at"] else "disabled")))
        limit="unlimited" if r["max_sessions"] == -1 else ("default" if not r["max_sessions"] else str(r["max_sessions"]))
        expires=r["expires_at"] or "never"
        online=int(active.get(status_client_key(r["id"]),0) or 0)
        print(f"{r['id']:<24} {state:<9} {online:>6} {limit:<10} {expires[:22]:<22} {r['label'][:22]}")
    print(f"Total: {len(rows)}")


def get_user(uid):
    validate_client_id(uid); con=db_connect(); r=con.execute("SELECT * FROM users WHERE id=?",(uid,)).fetchone(); con.close()
    if not r: die(f"user not found: {uid}")
    return r


def user_show(args):
    r=get_user(args.id)
    d=dict(r); d.pop("psk",None)
    if d.get("auth_handle") is not None: d["auth_handle"]=bytes(d["auth_handle"]).hex()
    expired=False
    if r["expires_at"]:
        try: expired=dt.datetime.fromisoformat(r["expires_at"].replace("Z","+00:00")) <= dt.datetime.now(dt.timezone.utc)
        except ValueError: expired=True
    d["state"]=("expired" if r["enabled"] and expired else ("enabled" if r["enabled"] else ("revoked" if r["revoked_at"] else "disabled")))
    d["session_limit"]=("unlimited" if r["max_sessions"] == -1 else ("server-default" if not r["max_sessions"] else r["max_sessions"]))
    print(json.dumps(d,indent=2))


def user_state(args, mode):
    require_root(); get_user(args.id); con=db_connect(); ts=now()
    with con:
        if mode=="enable": con.execute("UPDATE users SET enabled=1,revoked_at=NULL,updated_at=? WHERE id=?",(ts,args.id))
        elif mode=="disable": con.execute("UPDATE users SET enabled=0,updated_at=? WHERE id=?",(ts,args.id))
        elif mode=="revoke": con.execute("UPDATE users SET enabled=0,revoked_at=?,updated_at=? WHERE id=?",(ts,ts,args.id))
    con.close(); sync_user_credential(args.id); print(f"{mode}: {args.id}")


def user_limit(args):
    require_root(); get_user(args.id)
    value=-1 if args.unlimited else args.max_sessions
    if value != -1 and (value is None or value < 1 or value > 10000): die("limit must be 1..10000 or --unlimited")
    con=db_connect(); ts=now()
    with con: con.execute("UPDATE users SET max_sessions=?,updated_at=? WHERE id=?",(value,ts,args.id))
    con.close(); sync_user_credential(args.id)
    print(f"sessions limit: {args.id} = {'unlimited' if value==-1 else value}")


def user_expiry(args):
    require_root(); get_user(args.id)
    if args.clear:
        value=None
    elif args.days is not None:
        if args.days <= 0: die("--days must be > 0")
        value=(dt.datetime.now(dt.timezone.utc)+dt.timedelta(days=args.days)).replace(microsecond=0).isoformat()
    elif args.at:
        value=normalize_expiry(args.at)
    else:
        die("specify --days N, --at RFC3339 or --clear")
    con=db_connect(); ts=now()
    with con: con.execute("UPDATE users SET expires_at=?,updated_at=? WHERE id=?",(value,ts,args.id))
    con.close(); sync_user_credential(args.id)
    print(f"expiry: {args.id} = {value or 'never'}")


def user_renew(args):
    require_root(); get_user(args.id)
    if args.days <= 0: die("days must be > 0")
    con=db_connect(); row=con.execute("SELECT expires_at FROM users WHERE id=?",(args.id,)).fetchone(); nowdt=dt.datetime.now(dt.timezone.utc); base=nowdt
    if row["expires_at"]:
        try:
            old=dt.datetime.fromisoformat(row["expires_at"].replace("Z","+00:00"))
            if old > base: base=old
        except ValueError: pass
    value=(base+dt.timedelta(days=args.days)).replace(microsecond=0).isoformat(); ts=now()
    with con: con.execute("UPDATE users SET enabled=1,revoked_at=NULL,expires_at=?,updated_at=? WHERE id=?",(value,ts,args.id))
    con.close(); sync_user_credential(args.id); print(f"renewed: {args.id} until {value}")


def user_rotate(args):
    require_root(); get_user(args.id); con=db_connect(); ts=now()
    with con:
        con.execute("UPDATE users SET psk=?,enabled=1,rotated_at=?,revoked_at=NULL,updated_at=? WHERE id=?",
                    (secrets.token_bytes(32),ts,ts,args.id))
    con.close(); sync_user_credential(args.id); print(f"Rotated: {args.id}")
    if args.print_link: print(build_link(args.id, args.name))


def user_delete(args):
    require_root()
    ids=list(dict.fromkeys(args.ids))
    if not ids:
        die("specify at least one user id")
    rows=[]
    for uid in ids:
        rows.append(get_user(uid))
    handles=[bytes(r["auth_handle"]).hex() for r in rows]
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
    cp=load_conf()
    for handle in handles: remove_credential_handle(handle,cp)
    for uid in ids:
        print(f"Deleted: {uid}")
    print(f"Deleted {len(ids)} user(s).")


def build_link(uid, name=None, network_debug=False, android_http_stack="cronet", deterministic_transport=False):
    cp=load_conf(); r=get_user(uid)
    pub=read_hex32(cp["server"]["server_public_key"]).hex()
    payload={
        "v":1,
        "name": name or r["label"] or uid,
        "client_id":bytes(r["auth_handle"]).hex(),
        "transport":{"url":cp["server"]["public_url"],"android_http_stack":android_http_stack},
        "crypto":{"psk":bytes(r["psk"]).hex(),"server_public_key":pub},
        "camouflage":camo_dict(cp,include_timing=True),
    }
    diagnostics={}
    if network_debug:
        diagnostics["network_debug"]=True
    if deterministic_transport:
        diagnostics["deterministic_transport"]=True
    if diagnostics:
        payload["diagnostics"]=diagnostics
    raw=json.dumps(payload,separators=(",",":"),ensure_ascii=False).encode()
    return "mbox://"+base64.urlsafe_b64encode(raw).decode().rstrip("=")+"#"+uid


def build_app_link(uid,name=None,network_debug=False,android_http_stack="cronet",deterministic_transport=False):
    cp=load_conf(); public=urlparse(cp["server"]["public_url"])
    if public.scheme != "https" or not public.hostname: die("Verified Android App Links require HTTPS public_url")
    nested=build_link(uid,name,network_debug,android_http_stack,deterministic_transport); host=public.hostname
    if public.port and public.port != 443: host=f"{host}:{public.port}"
    return f"https://{host}/mbox/provision#profile={quote(nested,safe='')}"


def user_link(args):
    print(build_link(args.id,args.name,args.network_debug,args.android_http_stack,args.deterministic_transport))


def user_app_link(args):
    print(build_app_link(args.id,args.name,args.network_debug,args.android_http_stack,args.deterministic_transport))


def user_qr(args):
    link=build_app_link(args.id,args.name,args.network_debug,args.android_http_stack,args.deterministic_transport) if args.app_link else build_link(args.id,args.name,args.network_debug,args.android_http_stack,args.deterministic_transport)
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
    sync_user_credential(args.new_id); print(f"Renamed: {args.id} -> {args.new_id}")

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
        with tempfile.TemporaryDirectory(prefix="mbox-restore-check-") as td:
            root=Path(td)
            with tarfile.open(p,"r:gz") as tf:
                names=tf.getnames()
                if "users.db" not in names: die("backup does not contain users.db")
                if not any(n.startswith("etc-mbox/") for n in names): die("backup does not contain etc-mbox")
                tf.extractall(root,filter="data")
            db=root/"users.db"; con=sqlite3.connect(db); integrity=con.execute("PRAGMA integrity_check").fetchone()[0]; users=con.execute("SELECT count(*) FROM users").fetchone()[0]; con.close()
            if integrity != "ok": die(f"restored SQLite integrity check failed: {integrity}")
            etc=root/"etc-mbox"; conf=etc/"mbox.conf"; runtime=etc/"server.json"
            if not conf.is_file() or not runtime.is_file(): die("restored backup misses mbox.conf/server.json")
            cp=configparser.ConfigParser(interpolation=None); cp.read(conf)
            if "server" not in cp or "camouflage" not in cp: die("restored mbox.conf is incomplete")
            json.loads(runtime.read_text()); print(f"OK: test restore succeeded; sqlite={integrity}; users={users}")
    except (tarfile.TarError,OSError,sqlite3.Error,json.JSONDecodeError) as e: die(f"invalid backup: {e}")


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
    require_root(); cp=load_conf(); ensure_origin_hmac_key(cp); validate_config(cp); save_conf(cp)
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
        "runtime_dir":cp["relay"]["runtime_dir"],
        "origin_hmac_secret_file":"origin-hmac.key",
    }
    (REGRU/"relay.json").write_text(json.dumps(obj,indent=2)+"\n"); os.chmod(REGRU/"relay.json",0o600)
    secret=ensure_origin_hmac_key(cp)
    shutil.copy2(secret,REGRU/"origin-hmac.key"); os.chmod(REGRU/"origin-hmac.key",0o600)
    site=REGRU/"site"
    if site.exists(): shutil.rmtree(site)
    if SITE_TEMPLATE.is_dir(): shutil.copytree(SITE_TEMPLATE,site)
    instructions=f"""MBox REG.RU/shared-hosting deployment bundle

Authoritative generated files on the MBox admin/origin host:
  {REGRU}/relay.fcgi
  {REGRU}/relay.json
  {REGRU}/site/index.html
  {REGRU}/site/404.html

Upload relay.fcgi + relay.json + origin-hmac.key into the shared-hosting web root. Keep origin-hmac.key mode 0600 and outside public static-file routing.
The decoy site files are optional and are deployed only with relay deploy --site.
They belong on the shared-hosting frontend, not on the MBox origin VPS.
The existing FastCGI routing/.htaccess is deployment-specific and is not overwritten automatically.
Public endpoint: {cp['server']['public_url']}
Origin target: {cp['server']['origin_url']}
Base path: {base}
"""
    (REGRU/"DEPLOY.txt").write_text(instructions)
    hashes=[]
    for rel in [Path("relay.fcgi"),Path("relay.json"),Path("origin-hmac.key"),Path("site/index.html"),Path("site/404.html")]:
        path=REGRU/rel
        if path.is_file():
            b=path.read_bytes(); hashes.append(f"{hashlib.sha256(b).hexdigest()}  {rel.as_posix()}")
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
    reverse_proxy @mbox_health {listen}

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


# SSH relay deployments are deliberately independent of `generate_relay_files()`:
# a staged origin may use an HMAC key that differs from /etc/mbox/mbox.conf.
# Deploying a generated/default key would silently break the running tunnel.

def _relay_deploy_path(value, label):
    if not value or not value.startswith('/') or os.path.normpath(value) != value or '\n' in value:
        die(f'{label} must be an absolute normalized path')
    if value == '/':
        die(f'{label} must not be /')
    return value


def _relay_deploy_payload(args):
    if not args.remote_private_dir:
        die('--remote-private-dir is required (must be outside the webroot)')
    if not args.origin_config:
        die('--origin-config is required; choose the actual active RC1 origin server.json')
    if not args.relay_binary:
        die('--relay-binary is required; do not silently use the old installed template')
    webroot = _relay_deploy_path(args.remote_dir, '--remote-dir')
    private = _relay_deploy_path(args.remote_private_dir, '--remote-private-dir')
    if os.path.commonpath((webroot, private)) == webroot:
        die('--remote-private-dir must be outside --remote-dir')
    if os.path.commonpath((private, webroot)) == private:
        die('--remote-private-dir must not be an ancestor of the webroot')

    cp = load_conf()
    validate_config(cp)
    runtime_file = Path(args.origin_config)
    if not runtime_file.is_file() or runtime_file.is_symlink():
        die(f'origin runtime config is missing or a symlink: {runtime_file}')
    try:
        runtime = json.loads(runtime_file.read_text())
        transport = runtime['transport']
        base = transport['base_path']
        key_path = Path(transport['origin_hmac_secret_file'])
    except (ValueError, KeyError, TypeError) as e:
        die(f'invalid origin runtime config: {e}')
    if base != cp['server']['base_path']:
        die('origin base_path does not match mbox.conf; refusing to change Android URLs')
    if not key_path.is_absolute() or key_path.is_symlink():
        die('origin HMAC key path must be absolute and not a symlink')
    try:
        info = key_path.stat()
        if not stat.S_ISREG(info.st_mode) or info.st_mode & 0o007:
            die('origin HMAC key must be regular and not world-accessible')
        key_bytes = key_path.read_bytes()
        if len(bytes.fromhex(key_bytes.decode().strip())) != 32:
            die('origin HMAC key must be exactly 32 bytes encoded as hex')
    except (OSError, UnicodeError, ValueError) as e:
        die(f'cannot read configured origin HMAC key: {e}')
    binary = Path(args.relay_binary)
    if not binary.is_file() or binary.is_symlink():
        die('relay binary does not exist or is a symlink')
    binary_bytes = binary.read_bytes()
    if len(binary_bytes) < 100_000 or not binary_bytes.startswith(b'\x7fELF'):
        die('relay binary is not a plausible Linux ELF executable')
    digest = hashlib.sha256(binary_bytes).hexdigest()
    if args.relay_sha256 and digest != args.relay_sha256.lower():
        die('relay SHA256 does not match --relay-sha256')

    cfg = {
        'target': cp['server']['origin_url'],
        'allowed_exact': [base + '/health'],
        'allowed_prefixes': [base + '/api/v1/'],
        'max_slots': int(cp['relay']['max_slots']),
        'max_body_bytes': int(cp['relay']['max_body_bytes']),
        'runtime_dir': private + '/locks',
        'origin_hmac_secret_file': private + '/origin-hmac.key',
    }
    # Prevent accidental changes to the only public hostname / URL.
    if not (1 <= cfg['max_slots'] <= 128 and 1024 <= cfg['max_body_bytes'] <= 16*1024*1024):
        die('relay max_slots or max_body_bytes outside RC1 limits')
    files = {
        'relay.fcgi': binary_bytes,
        'relay.json': (json.dumps(cfg, indent=2) + '\n').encode(),
        'origin-hmac.key': key_bytes,
    }
    if args.site:
        for name in ('index.html', '404.html'):
            p = SITE_TEMPLATE / name
            if not p.is_file():
                die(f'decoy page is missing: {p}')
            files[name] = p.read_bytes()
    return webroot, private, digest, files


# This code executes under the REG.RU SSH account, not as root. It receives a
# small tar archive over encrypted SSH stdin, avoiding persistent secret copies
# in /tmp or the webroot. It leaves a complete private rollback snapshot.
_RELAY_SSH_DEPLOY = r'''
import hashlib, io, json, os, shutil, stat, sys, tarfile, tempfile, time
from pathlib import Path

os.umask(0o077)
web, private = (Path(x) for x in sys.argv[1:3])
if not web.is_dir() or web.is_symlink():
    raise SystemExit('DEPLOY_FAILED: webroot missing or symlink')
if not private.is_dir() or private.is_symlink():
    raise SystemExit('DEPLOY_FAILED: private directory missing or symlink')
web, private = web.resolve(), private.resolve()
if os.path.commonpath((str(web), str(private))) in (str(web), str(private)):
    raise SystemExit('DEPLOY_FAILED: webroot and private directory overlap')
if stat.S_IMODE(private.stat().st_mode) != 0o700 or private.stat().st_uid != os.geteuid():
    raise SystemExit('DEPLOY_FAILED: private directory must be owned by SSH user, mode 0700')
locks = private / 'locks'
if locks.exists() and (locks.is_symlink() or not locks.is_dir()):
    raise SystemExit('DEPLOY_FAILED: unsafe locks directory')
locks.mkdir(mode=0o700, exist_ok=True)
if stat.S_IMODE(locks.stat().st_mode) != 0o700 or locks.stat().st_uid != os.geteuid():
    raise SystemExit('DEPLOY_FAILED: locks directory must have mode 0700')

with tarfile.open(fileobj=io.BytesIO(sys.stdin.buffer.read()), mode='r:gz') as tf:
    members = tf.getmembers()
    allowed = {'relay.fcgi', 'relay.json', 'origin-hmac.key', 'index.html', '404.html'}
    names = {m.name for m in members}
    if len(names) != len(members) or not {'relay.fcgi','relay.json','origin-hmac.key'} <= names or not names <= allowed:
        raise SystemExit('DEPLOY_FAILED: unexpected archive members')
    if any(not m.isfile() or m.size > 20_000_000 for m in members):
        raise SystemExit('DEPLOY_FAILED: unsafe archive member')
    payload = {m.name: tf.extractfile(m).read() for m in members}

if not payload['relay.fcgi'].startswith(b'\x7fELF'):
    raise SystemExit('DEPLOY_FAILED: invalid ELF relay')
try:
    config = json.loads(payload['relay.json'])
    key = bytes.fromhex(payload['origin-hmac.key'].decode().strip())
    if len(key) != 32:
        raise ValueError('bad key size')
    if config['origin_hmac_secret_file'] != str(private / 'origin-hmac.key') or config['runtime_dir'] != str(locks):
        raise ValueError('private path mismatch')
except (ValueError, KeyError, UnicodeError) as e:
    raise SystemExit('DEPLOY_FAILED: invalid configuration/secret: ' + str(e))

# Refuse to deploy into a different site or unexpectedly change its origin.
current_config = web / 'relay.json'
if current_config.is_file():
    try:
        current = json.loads(current_config.read_text())
        if current.get('target') != config.get('target') or set(current.get('allowed_prefixes', [])) != set(config.get('allowed_prefixes', [])):
            raise ValueError('different relay target/allowed path prefixes')
    except (OSError, ValueError) as e:
        raise SystemExit('DEPLOY_FAILED: existing webroot config mismatch: ' + str(e))

for name in payload:
    p = web / name if name != 'origin-hmac.key' else private / name
    if p.is_symlink() or (p.exists() and not p.is_file()):
        raise SystemExit('DEPLOY_FAILED: unsafe destination: ' + name)

backups = private / 'backups'
backups.mkdir(mode=0o700, exist_ok=True)
if backups.is_symlink() or stat.S_IMODE(backups.stat().st_mode) != 0o700:
    raise SystemExit('DEPLOY_FAILED: unsafe backups directory')
backup_id = time.strftime('%Y%m%d-%H%M%S', time.gmtime()) + '-' + os.urandom(4).hex()
backup = backups / backup_id
backup.mkdir(mode=0o700)
originals = {}
for name in payload:
    target = private / name if name == 'origin-hmac.key' else web / name
    originals[name] = target.exists()
    if target.exists():
        shutil.copyfile(target, backup / name)
        os.chmod(backup / name, 0o600)
(backup / 'manifest.json').write_text(json.dumps(originals))
os.chmod(backup / 'manifest.json', 0o600)

with tempfile.TemporaryDirectory(prefix='.mbox-upload-', dir=str(private)) as tmp:
    tmp = Path(tmp)
    for name, content in payload.items():
        p = tmp / name
        p.write_bytes(content)
        os.chmod(p, 0o755 if name == 'relay.fcgi' else 0o600)
    replaced = []
    try:
        # The key must exist before the new configuration/binary are published.
        for name in ('origin-hmac.key', 'relay.json', 'relay.fcgi', 'index.html', '404.html'):
            if name not in payload:
                continue
            target = private / name if name == 'origin-hmac.key' else web / name
            # os.replace across filesystems is not guaranteed; stage each file
            # in its final directory, then rename into place.
            with tempfile.NamedTemporaryFile(dir=str(target.parent), prefix='.mbox-new-', delete=False) as f:
                staged = Path(f.name)
                f.write(payload[name])
                f.flush()
                os.fsync(f.fileno())
            os.chmod(staged, 0o755 if name == 'relay.fcgi' else 0o600)
            os.replace(staged, target)
            replaced.append(name)
        assert hashlib.sha256((web / 'relay.fcgi').read_bytes()).digest() == hashlib.sha256(payload['relay.fcgi']).digest()
        assert (private / 'origin-hmac.key').read_bytes() == payload['origin-hmac.key']
        assert json.loads((web / 'relay.json').read_text()) == config
    except BaseException:
        for name in reversed(replaced):
            target = private / name if name == 'origin-hmac.key' else web / name
            if originals[name]:
                with tempfile.NamedTemporaryFile(dir=str(target.parent), prefix='.mbox-restore-', delete=False) as f:
                    f.write((backup / name).read_bytes())
                    restore = Path(f.name)
                os.chmod(restore, 0o755 if name == 'relay.fcgi' else 0o600)
                os.replace(restore, target)
            else:
                target.unlink(missing_ok=True)
        raise

print('DEPLOY_OK')
print('BACKUP_ID=' + backup_id)
print('RELAY_SHA256=' + hashlib.sha256(payload['relay.fcgi']).hexdigest())
print('SECRET_OUTSIDE_WEBROOT=yes')
'''


_RELAY_SSH_ROLLBACK = r'''
import json, os, shutil, stat, sys, tempfile
from pathlib import Path
os.umask(0o077)
web, private = (Path(x) for x in sys.argv[1:3])
backup_id = sys.argv[3]
if not web.is_dir() or web.is_symlink() or not private.is_dir() or private.is_symlink():
    raise SystemExit('ROLLBACK_FAILED: unsafe webroot/private directory')
web, private = web.resolve(), private.resolve()
if os.path.commonpath((str(web), str(private))) in (str(web), str(private)):
    raise SystemExit('ROLLBACK_FAILED: paths overlap')
if private.stat().st_uid != os.geteuid() or stat.S_IMODE(private.stat().st_mode) != 0o700:
    raise SystemExit('ROLLBACK_FAILED: private directory not owned/mode 0700')
root = private / 'backups'
if not root.is_dir() or root.is_symlink():
    raise SystemExit('ROLLBACK_FAILED: no backups directory')
if backup_id == 'latest':
    candidates = sorted(x for x in root.iterdir() if x.is_dir() and not x.is_symlink())
    if not candidates:
        raise SystemExit('ROLLBACK_FAILED: no backups')
    backup = max(candidates, key=lambda x: x.stat().st_mtime_ns)
else:
    if not backup_id or any(c not in '0123456789-abcdef' for c in backup_id):
        raise SystemExit('ROLLBACK_FAILED: bad backup id')
    backup = root / backup_id
if not backup.is_dir() or backup.is_symlink():
    raise SystemExit('ROLLBACK_FAILED: backup missing')
manifest = json.loads((backup / 'manifest.json').read_text())
allowed = {'relay.fcgi', 'relay.json', 'origin-hmac.key', 'index.html', '404.html'}
if not manifest or not set(manifest) <= allowed or any(type(v) is not bool for v in manifest.values()):
    raise SystemExit('ROLLBACK_FAILED: invalid backup manifest')
for name, existed in manifest.items():
    target = private / name if name == 'origin-hmac.key' else web / name
    if target.is_symlink():
        raise SystemExit('ROLLBACK_FAILED: destination symlink')
    if existed and not (backup / name).is_file():
        raise SystemExit('ROLLBACK_FAILED: backup incomplete')
for name in ('origin-hmac.key', 'relay.json', 'relay.fcgi', 'index.html', '404.html'):
    if name not in manifest:
        continue
    target = private / name if name == 'origin-hmac.key' else web / name
    if manifest[name]:
        with tempfile.NamedTemporaryFile(dir=str(target.parent), prefix='.mbox-rollback-', delete=False) as f:
            f.write((backup / name).read_bytes())
            temp = Path(f.name)
        os.chmod(temp, 0o755 if name == 'relay.fcgi' else 0o600)
        os.replace(temp, target)
    else:
        target.unlink(missing_ok=True)
print('ROLLBACK_OK')
print('BACKUP_ID=' + backup.name)
'''


def _relay_ssh_call(host, script, args, data=None, retry_transport=0):
    # Paths and code are quoted as separate shell arguments for the remote SSH
    # command string. Do not use a shell on the *local* host.
    # Never automatically repeat a deploy: the remote side may have committed
    # before SSH disconnected. Restoring a known backup ID is idempotent.
    if retry_transport and (script != _RELAY_SSH_ROLLBACK or data is not None):
        raise ValueError('SSH transport retries are permitted only for rollback')
    cmd = 'python3 -c ' + shlex.quote(script) + ' ' + ' '.join(shlex.quote(x) for x in args)
    for attempt in range(retry_transport + 1):
        try:
            result = subprocess.run(['ssh', host, cmd], input=data, check=True,
                                    timeout=180, stdout=subprocess.PIPE,
                                    stderr=subprocess.PIPE)
            break
        except subprocess.CalledProcessError as exc:
            # OpenSSH uses 255 for connection failures. Remote validation
            # failures must not be retried; only transport interruptions can.
            if exc.returncode == 255 and attempt < retry_transport:
                eprint(f'rollback SSH disconnected; retrying ({attempt + 2}/{retry_transport + 1})')
                time.sleep(2)
                continue
            stderr = (exc.stderr or b'').decode('utf-8', errors='replace')
            lines = [line.strip() for line in stderr.splitlines() if line.strip()]
            reason = next((line for line in reversed(lines)
                           if 'ROLLBACK_FAILED:' in line or 'DEPLOY_FAILED:' in line),
                          lines[-1] if lines else 'remote command failed')
            die(f'relay SSH failed (exit {exc.returncode}): {reason[:350]}')
        except subprocess.TimeoutExpired:
            if attempt < retry_transport:
                eprint(f'rollback SSH timed out; retrying ({attempt + 2}/{retry_transport + 1})')
                time.sleep(2)
                continue
            die('relay SSH timed out; inspect the remote host before retrying')
        except OSError as exc:
            die(f'cannot start SSH: {exc}')
    output = result.stdout.decode('utf-8', errors='replace')
    if output:
        print(output, end='')
    return output


def _relay_public_health():
    from urllib.request import urlopen
    cp = load_conf()
    url = cp['server']['public_url'].rstrip('/') + '/health'
    with urlopen(url, timeout=15) as response:
        if response.status != 200:
            raise RuntimeError(f'public health returned HTTP {response.status}')
        # A healthy MBox endpoint can return b'ok', not just b'ready'.
        response.read(128)


def _relay_wait_public_health(attempts=12, delay_seconds=3):
    # FastCGI on shared hosting may briefly return 500 while workers are being
    # recycled after an atomic executable/config replacement. Do not trigger
    # rollback on a single transient probe failure.
    if attempts < 1 or delay_seconds < 0:
        raise ValueError('invalid relay health retry settings')
    last_error = None
    for attempt in range(attempts):
        try:
            _relay_public_health()
            if attempt:
                print(f'PUBLIC_HEALTH_RECOVERED_AFTER_ATTEMPTS={attempt + 1}')
            return
        except Exception as exc:
            last_error = exc
            if attempt + 1 < attempts:
                eprint(f'public health not ready ({attempt + 1}/{attempts}): {type(exc).__name__}')
                time.sleep(delay_seconds)
    raise RuntimeError(f'public health failed on {attempts} attempts: {last_error}')


def relay_deploy(args):
    require_root()
    if args.ftp_host:
        die('FTPS relay deploy is disabled for HMAC-protected RC1: it cannot guarantee a private key outside webroot. Use --ssh')
    if not args.ssh or not args.remote_dir:
        die('relay deploy requires --ssh and --remote-dir')
    webroot, private, digest, files = _relay_deploy_payload(args)
    if args.dry_run:
        print('DEPLOY PREFLIGHT OK (dry run; nothing transferred or modified)')
        print('RELAY_SHA256=' + digest)
        print('ORIGIN_CONFIG=' + args.origin_config)
        print('REMOTE_WEBROOT=' + webroot)
        print('REMOTE_PRIVATE=' + private)
        return
    import io
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode='w:gz') as tf:
        for name, content in files.items():
            member = tarfile.TarInfo(name)
            member.mode = 0o755 if name == 'relay.fcgi' else 0o600
            member.size = len(content)
            tf.addfile(member, io.BytesIO(content))
    output = _relay_ssh_call(args.ssh, _RELAY_SSH_DEPLOY, (webroot, private), buffer.getvalue())
    backup_ids = re.findall(r'^BACKUP_ID=([0-9]{8}-[0-9]{6}-[0-9a-f]{8})$', output, flags=re.M)
    if len(backup_ids) != 1 or 'DEPLOY_OK' not in output:
        die('remote deployment did not return a verifiable backup ID; inspect remote files before retrying')
    if not args.skip_health_check:
        try:
            _relay_wait_public_health()
        except Exception as exc:
            backup_id = backup_ids[0]
            eprint(f'public health failed after retries: {exc}; restoring backup {backup_id}')
            try:
                _relay_ssh_call(args.ssh, _RELAY_SSH_ROLLBACK,
                                (webroot, private, backup_id), retry_transport=2)
            except SystemExit:
                die('AUTOMATIC_ROLLBACK_NOT_CONFIRMED: inspect REG.RU before retrying '
                    f'(backup ID: {backup_id})')
            die('public health failed; previous relay restored. Verify Caddy/origin before retrying')
        print('PUBLIC_HEALTH_HTTP_200')


def relay_rollback(args):
    require_root()
    if not args.ssh or not args.remote_dir or not args.remote_private_dir:
        die('relay rollback requires --ssh, --remote-dir, --remote-private-dir')
    webroot = _relay_deploy_path(args.remote_dir, '--remote-dir')
    private = _relay_deploy_path(args.remote_private_dir, '--remote-private-dir')
    if os.path.commonpath((webroot, private)) in (webroot, private):
        die('private directory must be separate from webroot')
    _relay_ssh_call(args.ssh, _RELAY_SSH_ROLLBACK,
                    (webroot, private, args.backup_id), retry_transport=2)


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
    host,port=cp["server"]["upstream"].rsplit(":",1)
    try:
        with socket.create_connection((host,int(port)),timeout=2): print(f"upstream: OK ({cp['server']['upstream']})")
    except OSError as e: print(f"upstream: FAILED ({cp['server']['upstream']}): {e}")
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
    os.chmod(out,0o600)
    # A tarball is not a backup until it can be restored and parsed. Run the
    # same isolated extraction/SQLite/config checks as `backup verify`.
    class VerifyArgs: path=str(out)
    backup_verify(VerifyArgs())
    print(out)


def backup_files(directory=Path("/root")):
    files=[]
    for pattern in ("mbox-backup-*.tar.gz","mbox-preinstall-*.tar.gz","mbox-pre-reinit-*.tar.gz"):
        files.extend(directory.glob(pattern))
    return sorted({p.resolve() for p in files if p.is_file()}, key=lambda p:p.stat().st_mtime, reverse=True)


def backup_list(args):
    for p in backup_files(Path(args.directory)):
        st=p.stat(); stamp=dt.datetime.fromtimestamp(st.st_mtime).astimezone().isoformat(timespec="seconds")
        print(f"{st.st_size:>12}  {stamp}  {p}")


def backup_prune(args):
    require_root()
    if args.keep < 1: die("--keep must be >= 1")
    files=backup_files(Path(args.directory))
    doomed=files[args.keep:]
    if not doomed:
        print("Nothing to prune.")
        return
    for p in doomed:
        print(f"remove {p}")
        if not args.dry_run: p.unlink()
    print(f"{'Would remove' if args.dry_run else 'Removed'} {len(doomed)} old backup(s); kept {min(args.keep,len(files))} newest.")


def android_assetlinks(args):
    raw=re.sub(r"[^0-9A-Fa-f]","",args.sha256_cert)
    if len(raw)!=64: die("--sha256-cert must be a SHA-256 signing-certificate fingerprint")
    fp=":".join(raw[i:i+2].upper() for i in range(0,64,2))
    body=[{"relation":["delegate_permission/common.handle_all_urls"],"target":{"namespace":"android_app","package_name":"io.mbox.android","sha256_cert_fingerprints":[fp]}}]
    text=json.dumps(body,indent=2)+"\n"
    if args.output:
        out=Path(args.output);out.parent.mkdir(parents=True,exist_ok=True);out.write_text(text);print(out)
    else: print(text,end="")


def resources_cmd(args):
    cp=load_conf(); validate_config(cp)
    pid=0
    try:
        out=run(["systemctl","show","-p","MainPID","--value",SERVICE],capture=True,check=False).stdout.strip()
        pid=int(out or 0)
    except Exception: pid=0
    print("Configured transport limits:")
    for k in DEFAULT_LIMITS:
        v=int(cp["limits"][k])
        if k.endswith("_bytes"): print(f"  {k}={v} ({v/1024/1024:.1f} MiB)")
        else: print(f"  {k}={v}")
    if pid>0 and Path(f"/proc/{pid}/status").exists():
        status={}
        for line in Path(f"/proc/{pid}/status").read_text().splitlines():
            if ":" in line:
                k,v=line.split(":",1); status[k]=v.strip()
        try: fds=len(list(Path(f"/proc/{pid}/fd").iterdir()))
        except OSError: fds=-1
        print(f"Process: pid={pid} rss={status.get('VmRSS','?')} peak={status.get('VmHWM','?')} threads={status.get('Threads','?')} fds={fds}")
        try:
            current=run(["systemctl","show","-p","MemoryCurrent","--value",SERVICE],capture=True,check=False).stdout.strip()
            peak=run(["systemctl","show","-p","MemoryPeak","--value",SERVICE],capture=True,check=False).stdout.strip()
            if current and current != "[not set]":
                cur=int(current); peak_i=int(peak) if peak.isdigit() else 0
                text=f"Cgroup memory: current={cur/1024/1024:.1f} MiB"
                if peak_i: text+=f" peak={peak_i/1024/1024:.1f} MiB"
                print(text)
        except Exception:
            pass
    else:
        print("Process: not running or /proc unavailable")
    rs=read_runtime_status()
    if rs:
        print(f"Runtime: sessions={rs.get('active_sessions','?')} streams={rs.get('active_streams','?')} queued_bytes={rs.get('queued_bytes','?')}")
    con=db_connect(); total=con.execute("SELECT count(*) FROM users").fetchone()[0]; enabled=con.execute("SELECT count(*) FROM users WHERE enabled=1").fetchone()[0]; unlimited=con.execute("SELECT count(*) FROM users WHERE enabled=1 AND max_sessions=-1").fetchone()[0]; con.close()
    print(f"Users: total={total} enabled={enabled} unlimited={unlimited}")


def status(args):
    service_action("status")
    if CONF.exists():
        cp=load_conf(); print(f"Admin config: {CONF}"); print(f"Runtime config: {RUNTIME}"); print(f"Users DB: {DB}"); print(f"REG.RU: {REGRU}")


def make_parser():
    p=argparse.ArgumentParser(prog="mboxctl",description="MBox administration CLI")
    sp=p.add_subparsers(dest="cmd",required=True)
    q=sp.add_parser("init"); q.add_argument("--force",action="store_true",help="destructive reinitialize: backup old MBox state, then create fresh config/keys/users"); q.add_argument("--public-url"); q.add_argument("--origin-url"); q.add_argument("--base-path"); q.add_argument("--first-user"); q.add_argument("--listen",default="127.0.0.1:18449"); q.add_argument("--upstream",default="127.0.0.1:2080"); q.add_argument("--max-slots",type=int,default=4); q.add_argument("--max-body-bytes",type=int,default=2097152); q.add_argument("--relay-runtime-dir",help="private 0700 runtime dir on the shared host; default is randomized"); q.set_defaults(func=init_cmd)
    m=sp.add_parser("migrate"); ms=m.add_subparsers(dest="sub",required=True); q=ms.add_parser("legacy"); q.add_argument("--server-json",default="/etc/mbox/server.json"); q.add_argument("--public-url"); q.add_argument("--origin-url"); q.set_defaults(func=migrate_legacy)
    u=sp.add_parser("user"); us=u.add_subparsers(dest="sub",required=True)
    q=us.add_parser("add"); q.add_argument("ids",nargs="*"); q.add_argument("--count",type=int); q.add_argument("--prefix",default="user-"); q.add_argument("--width",type=int); q.add_argument("--note"); q.add_argument("--max-sessions","--max-devices",dest="max_sessions",type=int); q.add_argument("--unlimited",action="store_true"); q.add_argument("--days",type=int); q.add_argument("--expires-at"); q.set_defaults(func=user_add)
    q=us.add_parser("import"); q.add_argument("csv_file"); q.set_defaults(func=user_import)
    q=us.add_parser("list"); q.add_argument("--enabled",action="store_true"); q.add_argument("--revoked",action="store_true"); q.set_defaults(func=user_list)
    q=us.add_parser("show"); q.add_argument("id"); q.set_defaults(func=user_show)
    q=us.add_parser("count"); q.set_defaults(func=user_count)
    q=us.add_parser("rename"); q.add_argument("id"); q.add_argument("new_id"); q.set_defaults(func=user_rename)
    for name in ["enable","disable","revoke"]:
        q=us.add_parser(name); q.add_argument("id"); q.set_defaults(func=lambda a,n=name:user_state(a,n))
    q=us.add_parser("rotate"); q.add_argument("id"); q.add_argument("--print-link",action="store_true"); q.add_argument("--name"); q.set_defaults(func=user_rotate)
    q=us.add_parser("limit"); q.add_argument("id"); q.add_argument("max_sessions",type=int,nargs="?"); q.add_argument("--unlimited",action="store_true"); q.set_defaults(func=user_limit)
    q=us.add_parser("devices"); q.add_argument("id"); q.add_argument("max_sessions",type=int,nargs="?"); q.add_argument("--unlimited",action="store_true"); q.set_defaults(func=user_limit)
    q=us.add_parser("expiry"); q.add_argument("id"); q.add_argument("--days",type=int); q.add_argument("--at"); q.add_argument("--clear",action="store_true"); q.set_defaults(func=user_expiry)
    q=us.add_parser("renew"); q.add_argument("id"); q.add_argument("days",type=int); q.set_defaults(func=user_renew)
    q=us.add_parser("delete"); q.add_argument("ids",nargs="+"); q.add_argument("-y","--yes",action="store_true",help="delete without interactive confirmation"); q.add_argument("--purge",action="store_true",help=argparse.SUPPRESS); q.set_defaults(func=user_delete)
    q=us.add_parser("link"); q.add_argument("id"); q.add_argument("--name"); q.add_argument("--network-debug",action="store_true"); q.add_argument("--android-http-stack",choices=["cronet","go"],default="cronet"); q.add_argument("--deterministic-transport",action="store_true"); q.set_defaults(func=user_link)
    q=us.add_parser("app-link"); q.add_argument("id"); q.add_argument("--name"); q.add_argument("--network-debug",action="store_true"); q.add_argument("--android-http-stack",choices=["cronet","go"],default="cronet"); q.add_argument("--deterministic-transport",action="store_true"); q.set_defaults(func=user_app_link)
    q=us.add_parser("qr"); q.add_argument("id"); q.add_argument("--name"); q.add_argument("--app-link",action="store_true"); q.add_argument("--network-debug",action="store_true"); q.add_argument("--android-http-stack",choices=["cronet","go"],default="cronet"); q.add_argument("--deterministic-transport",action="store_true"); q.set_defaults(func=user_qr)
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
    q=rs.add_parser("deploy"); q.add_argument("--ssh"); q.add_argument("--ftp-host"); q.add_argument("--ftp-user"); q.add_argument("--ftp-plain",action="store_true"); q.add_argument("--remote-dir"); q.add_argument("--remote-private-dir"); q.add_argument("--origin-config"); q.add_argument("--relay-binary"); q.add_argument("--relay-sha256"); q.add_argument("--dry-run",action="store_true"); q.add_argument("--skip-health-check",action="store_true",help="disable automatic HTTP health check and rollback (only for maintenance)"); q.add_argument("--site",action="store_true",help="also deploy decoy index.html and 404.html to the shared-hosting web root"); q.set_defaults(func=relay_deploy)
    q=rs.add_parser("rollback"); q.add_argument("--ssh",required=True); q.add_argument("--remote-dir",required=True); q.add_argument("--remote-private-dir",required=True); q.add_argument("--backup-id",default="latest"); q.set_defaults(func=relay_rollback)
    q=rs.add_parser("test"); q.set_defaults(func=relay_test)
    s=sp.add_parser("server"); ss=s.add_subparsers(dest="sub",required=True)
    for name in ["start","stop","restart","status"]:
        q=ss.add_parser(name); q.set_defaults(func=lambda a,n=name: service_action(n))
    q=sp.add_parser("sync"); q.add_argument("--restart",action="store_true"); q.set_defaults(func=lambda a: render_runtime(reload_auth=not a.restart,restart=a.restart))
    q=sp.add_parser("doctor"); q.set_defaults(func=doctor)
    q=sp.add_parser("status"); q.set_defaults(func=status)
    q=sp.add_parser("resources"); q.set_defaults(func=resources_cmd)
    q=sp.add_parser("logs"); q.add_argument("-f","--follow",action="store_true"); q.add_argument("-n","--lines",type=int,default=100); q.set_defaults(func=logs_cmd)
    d=sp.add_parser("db"); ds=d.add_subparsers(dest="sub",required=True); q=ds.add_parser("check"); q.set_defaults(func=db_check); q=ds.add_parser("vacuum"); q.set_defaults(func=db_vacuum)
    l=sp.add_parser("link"); ls=l.add_subparsers(dest="sub",required=True); q=ls.add_parser("inspect"); q.add_argument("path"); q.add_argument("--redact",action="store_true"); q.set_defaults(func=link_inspect)
    a=sp.add_parser("android"); aas=a.add_subparsers(dest="sub",required=True); q=aas.add_parser("assetlinks"); q.add_argument("--sha256-cert",required=True); q.add_argument("--output"); q.set_defaults(func=android_assetlinks)
    b=sp.add_parser("backup"); bs=b.add_subparsers(dest="sub",required=True)
    q=bs.add_parser("create"); q.add_argument("--output"); q.set_defaults(func=backup_create)
    q=bs.add_parser("verify"); q.add_argument("path"); q.set_defaults(func=backup_verify)
    q=bs.add_parser("list"); q.add_argument("--directory",default="/root"); q.set_defaults(func=backup_list)
    q=bs.add_parser("prune"); q.add_argument("--directory",default="/root"); q.add_argument("--keep",type=int,default=3); q.add_argument("--dry-run",action="store_true"); q.set_defaults(func=backup_prune)
    q=sp.add_parser("version"); q.set_defaults(func=lambda a: print("mboxctl 1.0.0-beta.2.2"))
    return p


def main():
    args=make_parser().parse_args(); rv=args.func(args); return 0 if rv is None else (rv if isinstance(rv,int) else 0)

if __name__=="__main__":
    raise SystemExit(main())
