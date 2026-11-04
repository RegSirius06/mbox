import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def test_github_actions_are_pinned_to_commit_sha():
    workflow = (ROOT / '.github/workflows/ci.yml').read_text()
    uses = re.findall(r'^\s*-\s+uses:\s+([^\s#]+)', workflow, flags=re.M)
    assert uses, 'expected GitHub Actions uses entries'
    for action in uses:
        ref = action.rsplit('@', 1)[-1]
        assert re.fullmatch(r'[0-9a-f]{40}', ref), f'unpinned action: {action}'


def test_ci_permissions_remain_read_only_by_default():
    workflow = (ROOT / '.github/workflows/ci.yml').read_text()
    assert re.search(r'^permissions:\n\s+contents:\s+read\s*$', workflow, flags=re.M)


def test_android_ci_does_not_request_removed_sdk_tools_package():
    workflow = (ROOT / '.github/workflows/ci.yml').read_text()
    action = 'android-actions/setup-android@9fc6c4e9069bf8d3d10b2204b1fb8f6ef7065407'
    assert action in workflow
    # The default action package list contains the obsolete Android SDK "tools".
    # An explicit supported package list keeps sdkmanager bootstrap working.
    suffix = workflow.split(action, 1)[1].split('      - run:', 1)[0]
    assert "packages: 'platform-tools'" in suffix


def test_gitleaks_fixture_allowlist_is_narrow_and_default_rules_remain():
    import tomllib
    with (ROOT / '.gitleaks.toml').open('rb') as fd:
        config = tomllib.load(fd)
    assert config['extend']['useDefault'] is True
    assert len(config['rules']) == 1
    assert config['rules'][0]['id'] == 'generic-api-key'
    assert len(config['rules'][0]['allowlists']) == 1
    allowed = config['rules'][0]['allowlists'][0]
    assert allowed['condition'] == 'AND'
    assert allowed['regexTarget'] == 'secret'
    assert len(allowed['paths']) == 3
    assert all(p.startswith('^') and p.endswith('$') for p in allowed['paths'])
    assert len(allowed['regexes']) == 2
    assert all(r.startswith('^') and r.endswith('$') for r in allowed['regexes'])
