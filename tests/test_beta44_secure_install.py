from pathlib import Path
import re
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def test_secure_new_install_defaults():
    text = (ROOT / "tools/mboxctl/mboxctl.py").read_text()
    assert '"require_authenticated_control": "true"' in text
    assert '"accept_legacy_empty_open": "false"' in text
    assert '"accept_legacy_client_ids": "true"' in text


def test_install_wizard_and_confirmation():
    s = (ROOT / "install.sh").read_text()
    assert "WIZARD=1" in s
    assert "Customize compatibility? [y/N]" in s
    assert "Change compatibility settings? [y/N]" in s
    assert "Type ALLOW-UNSIGNED to continue" in s
    assert "if [[ ! -t 0 || ! -t 1 ]]; then" in s
    assert "LEGACY_CONTROL_MODE=true" in s


def test_unattended_unsafe_controls_rejected():
    r = subprocess.run(
        ["bash", str(ROOT / "install.sh"), "--check", "--legacy-unsigned-control=on"],
        cwd=ROOT, capture_output=True, text=True, input=""
    )
    assert r.returncode != 0
    assert "REFUSED" in r.stderr or "Compatibility switches require" in r.stderr


def test_install_bash_syntax():
    subprocess.run(["bash", "-n", str(ROOT / "install.sh")], check=True)
