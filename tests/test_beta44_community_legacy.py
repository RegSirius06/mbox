from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]


def test_telegram_is_next_to_github_in_home_footer():
    main = (ROOT / "android/app/src/main/java/io/mbox/android/MainActivity.java").read_text()
    footer = main.split("LinearLayout communityLinks = new LinearLayout(this);", 1)[1].split("} else {", 1)[0]
    assert '"Telegram · Community"' in footer
    assert '"GitHub · Releases"' in footer
    assert 'openExternalPage("https://t.me/github_mailbox")' in footer
    settings = main.split("private void showSettingsHome()", 1)[1].split("private void showProfileHub()", 1)[0]
    assert 'addPageAction(content, "Telegram community"' not in settings


def test_installer_compatibility_switches_are_explicit_only():
    install = (ROOT / "install.sh").read_text()
    assert "LEGACY_IDS_MODE=""" in install
    assert "LEGACY_EMPTY_MODE=""" in install
    assert "LEGACY_CONTROL_MODE=""" in install
    for flag in ('--legacy-client-ids=on', '--legacy-client-ids=off',
                 '--legacy-empty-open=on', '--legacy-empty-open=off',
                 '--legacy-unsigned-control=on', '--legacy-unsigned-control=off'):
        assert flag in install
    assert 'if [[ -n "$LEGACY_IDS_MODE$LEGACY_EMPTY_MODE$LEGACY_CONTROL_MODE" ]]' in install
    assert 'mboxctl sync >/dev/null' in install


def test_legacy_open_defaults_on_without_downgrading_existing_security():
    ctl = (ROOT / "tools/mboxctl/mboxctl.py").read_text()
    assert '"accept_legacy_client_ids": "true"' in ctl
    assert '"accept_legacy_empty_open": "false"' in ctl
    assert '"require_authenticated_control": "true"' in ctl
    config = (ROOT / "server/config_runtime.go").read_text()
    assert 'cfg.Crypto.AcceptLegacyEmptyOpen != nil && *cfg.Crypto.AcceptLegacyEmptyOpen' in config
