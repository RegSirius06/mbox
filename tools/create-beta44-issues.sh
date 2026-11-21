#!/usr/bin/env bash
# Explicit, idempotent GitHub issue publication. Does not publish a release.
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
REPO=RegSirius06/mbox
command -v gh >/dev/null || { echo 'Install/authenticate GitHub CLI (gh), or create issues manually.' >&2; exit 1; }
gh auth status >/dev/null
read -r -p 'Create public beta.4.4 issues on RegSirius06/mbox? Type CREATE: ' confirmation
[[ "$confirmation" == CREATE ]] || { echo CANCELLED; exit 1; }
for label in 'type:bug' 'type:performance' 'type:testing' 'type:ux' 'priority:high' 'priority:medium' 'priority:low' 'beta.4.4'; do
 gh label create "$label" --repo "$REPO" --color 'd4d4d4' --force >/dev/null
done
while IFS='|' read -r file title labels; do
 [[ -n "$file" ]] || continue
 # Exact title search prevents duplicate publication on reruns.
 if gh issue list --repo "$REPO" --state all --limit 1000 --json title --jq '.[].title' | grep -Fqx "$title"; then
   echo "SKIP existing issue: $title"
 else
   gh issue create --repo "$REPO" --title "$title" --body-file "$ROOT/docs/release-issues/$file" --label "$labels"
 fi
done <<'ISSUES'
fastcgi-cpu.md|REG.RU FastCGI CPU/worker exhaustion risk|type:performance,priority:high,beta.4.4
android-battery.md|Measure Android battery impact of on-demand DATAGRAM|type:performance,priority:medium,beta.4.4
network-leaks.md|Validate physical IPv4/IPv6/DNS/UDP leaks and Lockdown|type:testing,priority:high,beta.4.4
github-updates.md|End-to-end update alert: beta.4.3 to beta.4.4|type:testing,priority:medium,beta.4.4
egress-default-ui.md|Admin UI: equal country exits and legacy default|type:ux,priority:low,beta.4.4
installer-e2e.md|Staging verification of install.sh fresh-install and rollback|type:testing,priority:high,beta.4.4
ISSUES
