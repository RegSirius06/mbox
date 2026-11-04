from pathlib import Path
import importlib.util

ROOT = Path(__file__).resolve().parents[1]
MODULE = ROOT / "tools" / "mboxctl" / "mboxctl.py"


def test_control_mac_requires_explicit_strict_opt_in():
    spec = importlib.util.spec_from_file_location("mboxctl_control_mac_contract", MODULE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    assert module.DEFAULT_AUTH["require_authenticated_control"] == "false"
    text = MODULE.read_text()
    assert '"require_authenticated_control": cp.getboolean("auth", "require_authenticated_control")' in text
    assert "auth.require_authenticated_control must be true or false" in text


def test_control_mac_staged_on_both_sides():
    client_url = (ROOT / "client" / "uplink.go").read_text()
    client_close = (ROOT / "client" / "http_transport.go").read_text()
    server = (ROOT / "server" / "http_handlers.go").read_text()
    assert 'q.Set("cmac", controlmeta.Tag' in client_url
    assert 'q.Set("cmac", controlmeta.Tag' in client_close
    assert "verifyHTTPControlMeta(s, r, \"UP\", seq, ack)" in server
    assert 'r.URL.Query().Get("cmac")' in server
