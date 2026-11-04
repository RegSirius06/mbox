from pathlib import Path
import shutil
import subprocess
import textwrap

ROOT = Path(__file__).resolve().parents[1]
SERVICE = ROOT / 'android/app/src/main/java/io/mbox/android/MBoxVpnService.java'
SOURCE = ROOT / 'android/app/src/main/java/io/mbox/android/MBoxLogSanitizer.java'


def test_android_error_log_reaches_sanitizer():
    source = SERVICE.read_text()
    assert '"core: " + MBoxLogSanitizer.sanitize(line)' in source
    assert '"VPN transport attempt failed: " + MBoxLogSanitizer.sanitize(message)' in source
    assert 'Log.w(TAG, "VPN transport attempt failed", error)' not in source


def test_android_java_log_sanitizer(tmp_path):
    if shutil.which('javac') is None or shutil.which('java') is None:
        # Source contract is still tested without local Java toolchain.
        return
    source = tmp_path / 'SanitizerSmoke.java'
    source.write_text(textwrap.dedent('''
        package io.mbox.android;
        public class SanitizerSmoke {
            public static void main(String[] args) {
                String[] samples = {
                    "Post \\\"https://example.fun/edge/private123/api/v1/session\\\": timeout",
                    "mbox://private-profile-link#name",
                    "Authorization: Bearer alpha123",
                    "Proxy-Authorization: Basic dXNlcjpwYXNz",
                    "psk=abcdef password:sosecret token=hidden",
                    "psk=abc=def",
                    "first\\nFORGED: another line"
                };
                for (String s : samples) System.out.println(MBoxLogSanitizer.sanitize(s));
            }
        }
    '''))
    subprocess.run(['javac', '-d', str(tmp_path), str(SOURCE), str(source)], check=True)
    result = subprocess.run(['java', '-cp', str(tmp_path), 'io.mbox.android.SanitizerSmoke'],
                            check=True, capture_output=True, text=True).stdout
    for secret in ('private123', 'example.fun', 'private-profile', 'alpha123', 'sosecret', 'abcdef', 'hidden', 'abc=def', 'dXNlcjpwYXNz'):
        assert secret not in result
    assert r'first\nFORGED' in result


def test_cronet_and_ui_error_paths_redact_before_display():
    cronet = (ROOT / 'android/app/src/main/java/io/mbox/android/CronetBridge.java').read_text()
    service = SERVICE.read_text()
    assert 'updateState(State.RECOVERING, MBoxLogSanitizer.sanitize(message))' in service
    assert 'Log.w(TAG, "Cronet bridge request failed", error)' not in cronet
    assert '"Cronet bridge request failed: " + MBoxLogSanitizer.sanitize(error.toString())' in cronet


def test_fail_closed_log_redaction():
    source = SERVICE.read_text()
    assert 'updateState(State.FAILED, MBoxLogSanitizer.sanitize(message))' in source
    assert '"Fail-closed VPN interface failed: " + MBoxLogSanitizer.sanitize(message)' in source
    assert 'MBoxLogSanitizer.sanitize(safeMessage(fatal))' in source
    assert 'lastError = MBoxLogSanitizer.sanitize(error);' in source
