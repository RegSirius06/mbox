# MBox beta.4.2 status (2026-11-15)

| Feature / test | Status | Evidence / limitations |
|---|---|---|
| Web admin on VPS | **Working** | Operator confirmed routine user operations and subscriptions |
| Browser `mbox.conf` editor | **Working in operator deployment** | Subscription path was persisted and applied to Caddy; required a systemd ReadWritePaths adjustment |
| Stable subscription links | **Working** | Reopening the primary URL does not mint another token; old tokens are not automatically revoked |
| Public HTTPS subscription endpoint | **Working** | Invalid-token response verified; valid-token profile imported in Android |
| Multiple Android subscriptions | **Implemented** | Android patch installed; full 2×2 scenario untested |
| Multiple HTTPS ingress profiles | **Implemented; multi-host not field-tested** | Only one ingress active in the operator deployment |
| Older direct `mbox://` via HTTPS | **Tested, works** | Maintainer's device test |
| Older QUIC provisioning | **Fails with current deployment** | TLS certificate trust mismatch plausible but not confirmed by diagnostic transcript |
| New HTTPS subscription import | **Tested, works** | Imported and connected in beta.4.2 |
| Signed Android APK | **In-place upgrade succeeded** | `versionCode 9`, `adb install -r Success`; final published artifact hash pending |
| QR preview | **Known issue** | Copy/paste of textual URLs works |
| Device registration / traffic metering | **Not implemented** | Concurrent-session limit only |
| Automatic HTTPS ingress failover | **Not implemented** | Manual endpoint selection requires stopped VPN |
| Physical leak / Lockdown / soak testing | **Not demonstrated** | Further acceptance tests required |
| Final GitHub CI / tag / prerelease | **Not confirmed** | Pending source synchronization and publishing checklist |

**Readiness:** usable in the maintainer's limited private deployment and suitable for prerelease preparation. General production and RC1 readiness are **not established**.

**Compatibility note:** older apps cannot consume new HTTPS subscription URLs. Older direct profiles continue over the tested HTTPS/FastCGI path; current QUIC requires an updated profile with valid TLS trust data. Do not disable certificate verification.
