# Verify in-VPN GitHub release update alert: beta.4.3 → beta.4.4

**Severity:** medium (P2). **Component:** Android ReleaseUpdates.

After publishing a **real** GitHub prerelease, keep the signed beta.4.3 APK installed, connect MBox, allow notification permission, and check that the app discovers the latest version through the established VPN only, offers a non-intrusive action to open the genuine GitHub release, can be dismissed and does not prompt repeatedly on every app start. Also verify no-VPN and notification-denied behavior, cached rate limit, GitHub API failure and incorrect/older version handling. Must never bypass fail-closed via direct HTTP.
