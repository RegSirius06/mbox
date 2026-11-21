"""Source contract: enabling release UDP must not require old laboratory flag."""
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_normal_udp_and_lab_udp_are_independent():
    code = (ROOT / "server/config_runtime.go").read_text()
    assert 'EnableUDP            bool   `json:"enable_udp,omitempty"`' in code
    assert 'enableUDPLabServer = cfg.Transport.EnableUDP || cfg.Transport.EnableUDPLab' in code
    cli = (ROOT / "tools/mboxctl/mboxctl.py").read_text()
    assert '"enable_udp": cp.getboolean("server", "enable_udp", fallback=False)' in cli
    assert 'server.enable_udp must be true or false' in cli
