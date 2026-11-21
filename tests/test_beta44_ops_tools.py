import fcntl
import importlib.util
import json
import os
import socket
import subprocess
import sys
import threading
from pathlib import Path
from types import SimpleNamespace

ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "tools" / "mboxctl" / "mboxctl.py"
spec = importlib.util.spec_from_file_location("mboxctl_beta44_ops", MODULE_PATH)
mboxctl = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mboxctl)


def test_relay_bench_uses_only_bounded_health_requests(monkeypatch, capsys):
    monkeypatch.setattr(mboxctl, "load_conf", lambda: {
        "server": {"public_url": "https://vpn.example/edge"},
        "relay": {"max_slots": 4},
    })
    monkeypatch.setattr(mboxctl, "validate_config", lambda cp: None)
    seen = []

    class Response:
        def __enter__(self):
            return self
        def __exit__(self, *args):
            return False
        def getcode(self):
            return 200
        def read(self, limit):
            return b"ok"

    def fake_urlopen(req, timeout, context):
        seen.append((req.full_url, req.method, timeout))
        return Response()

    monkeypatch.setattr(mboxctl, "urlopen", fake_urlopen)
    rc = mboxctl.relay_bench(SimpleNamespace(
        confirm=True, requests=12, concurrency=3, timeout=4, rps=20
    ))
    output = capsys.readouterr().out
    assert rc == 0
    assert len(seen) == 12
    assert all(url == "https://vpn.example/edge/health" for url, _, _ in seen)
    assert all(method == "GET" for _, method, _ in seen)
    assert "no user session" in output
    assert "HTTP_200=12" in output


def test_relay_bench_requires_explicit_confirmation(monkeypatch):
    monkeypatch.setattr(mboxctl, "load_conf", lambda: (_ for _ in ()).throw(AssertionError("must not load config")))
    try:
        mboxctl.relay_bench(SimpleNamespace(confirm=False))
    except SystemExit:
        pass
    else:
        raise AssertionError("bench ran without --confirm")


def test_relay_stats_is_read_only_on_demand(monkeypatch, capsys):
    seen = {}
    monkeypatch.setattr(mboxctl, "require_root", lambda: None)
    monkeypatch.setattr(mboxctl, "_relay_profile_paths",
                        lambda args: ("hosting-user@example", "/web", "/private"))
    def fake_ssh(host, script, args):
        seen.update(host=host, script=script, args=args)
        return "MAX_SLOTS=4\nACTIVE_SLOTS=0\n"
    monkeypatch.setattr(mboxctl, "_relay_ssh_call", fake_ssh)
    mboxctl.relay_stats(SimpleNamespace())
    assert seen["host"] == "hosting-user@example"
    assert seen["args"] == ("/web", "/private")
    assert "ps" in seen["script"]
    assert "/proc/locks" in seen["script"]
    assert "kill" not in seen["script"]
    assert "unlink" not in seen["script"]
    assert "ACTIVE_SLOTS=0" in capsys.readouterr().out


def test_admin_status_checks_private_unix_socket_without_public_listener(monkeypatch, tmp_path, capsys):
    sock_path = tmp_path / "admin.sock"
    listener = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    listener.bind(str(sock_path))
    sock_path.chmod(0o600)
    listener.listen(1)
    monkeypatch.setattr(mboxctl, "ADMIN_SOCKET", sock_path)
    monkeypatch.setattr(mboxctl, "require_root", lambda: None)
    monkeypatch.setattr(mboxctl.subprocess, "run",
                        lambda *a, **k: subprocess.CompletedProcess(a[0], 0))
    done = threading.Event()

    def serve():
        conn, _ = listener.accept()
        try:
            conn.recv(4096)
            conn.sendall(b"HTTP/1.0 200 OK\r\nContent-Length: 0\r\n\r\n")
        finally:
            conn.close()
            done.set()

    thread = threading.Thread(target=serve)
    thread.start()
    try:
        assert mboxctl.admin_status(SimpleNamespace()) == 0
        assert done.wait(2)
    finally:
        listener.close()
        thread.join(timeout=2)
    output = capsys.readouterr().out
    assert "ADMIN_SERVICE=active" in output
    assert "ADMIN_BACKEND_HTTP=200" in output
    assert "SSH-only" in output
    assert "ssh -N -L 8765:" in output


def test_remote_stats_reports_active_flock_without_mutating_it(tmp_path):
    import pytest
    if not os.access("/proc/locks", os.R_OK):
        pytest.skip("/proc/locks is unavailable")
    web = tmp_path / "web"
    private = tmp_path / "private"
    locks = private / "locks"
    web.mkdir()
    locks.mkdir(parents=True)
    locks.chmod(0o700)
    (web / "relay.json").write_text(json.dumps({"max_slots": 4}))
    lock_path = locks / "slot-000.lock"
    lock_path.touch(mode=0o600)
    with lock_path.open("r+") as lock_file:
        fcntl.flock(lock_file.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        script = mboxctl._RELAY_SSH_STATS
        result = subprocess.run(
            [sys.executable, "-c", script, str(web), str(private)],
            capture_output=True, text=True, timeout=10, check=True
        )
        assert "MAX_SLOTS=4" in result.stdout
        assert "ACTIVE_SLOTS=1" in result.stdout
        assert "WORKERS=" in result.stdout
