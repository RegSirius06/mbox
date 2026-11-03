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
