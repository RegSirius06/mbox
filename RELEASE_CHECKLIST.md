# MBox v1.0.0-beta.4 release checklist

[Русская версия](RELEASE_CHECKLIST.ru.md) · [Notes](RELEASE_NOTES.md)

This checklist does not deploy to any VPS/shared host or publish a GitHub release by itself.

1. Confirm `VERSION=1.0.0-beta.4`; review `git status`, `git diff --check`, Go race/vet suites, `python3 -m pytest -q tests`, and `./tools/secret-scan.sh`. Review legitimate literal `mbox://` findings separately from real credentials.
2. Check `dist/release/MBox-1.0.0-beta.4-android-arm64.apk`: release, not debuggable, `versionCode=7`, both native libraries and pinned signer using `tools/check-production-apk.sh`. SHA-256 of the **observed candidate** was `c3b1cbcb1b4b472aba35237657c2834a2aa8f032d861cbf817f5a5b9f18b43e1`; rebuilds require new hashes and release notes.
3. Test the **signed release APK**, not just debug: profile import, manual QUIC, HTTPS, Auto/network switching, connectivity, DNS/UDP, Lockdown as available. Mark incomplete physical leak/soak/carrier tests as incomplete.
4. Review the staged Git diff, exclude credentials, commit source and documentation. Ensure the final commit is merged into `main` with green CI, then tag **that exact commit** `v1.0.0-beta.4`, push the branch and tag. Never blindly `git add -A` or force-move a published tag.
5. Generate `sha256sum "$APK" > "$APK.sha256"` and publish GitHub **prerelease** with the APK, checksum and `docs/GITHUB_RELEASE_BODY.md`. Use GitHub CLI `gh release create ... --verify-tag --prerelease --draft`; publish the reviewed draft with `gh release edit v1.0.0-beta.4 --draft=false`.
6. Renew the deployment QUIC certificate before **2026-12-07** and distribute trusted updated profiles. This server-specific operation is not part of APK release automation.

See [Russian commands](RELEASE_CHECKLIST.ru.md) for a complete example.
