# MBox documentation — 1.0.0-beta.4.3

[Русский](ru/README.md) · [Home](../README.md)

**New to MBox?** Start here; the historical beta.2–beta.4.2 pages remain available for reference, but are **not** current release instructions.

## 1. First steps

- [Android: install, connect, update, troubleshoot](GETTING_STARTED.md)
- [First-time self-hosted server setup](SERVER_QUICKSTART.md)
- [Android guide / supported profiles](ANDROID_MULTI_SUBSCRIPTIONS.md)
- [Current release notes](../RELEASE_NOTES.md)

## 2. Server operator

- [Administration / CLI](ADMIN.md) · [Optional HTTPS admin domain](MBOX_ADMIN_PUBLIC_ACCESS.md)
- [Subscriptions and ingress](MBOX_SUBSCRIPTIONS.md)
- [REG.RU shared hosting](REG_RU_SHARED_HOSTING.md) · [Safe SSH FastCGI relay deployment](SAFE_SSH_RELAY_DEPLOY.md)
- [TLS certificates and renewal](QUIC_TLS_ROTATION.md)
- [Installation wizard](BETA4_3_INSTALL_WIZARD.md)

## 3. Architecture and safety

- [Architecture](ARCHITECTURE.md) · [QUIC and HTTPS](BETA_4_TRANSPORTS.md)
- [Security limits](SECURITY.md) · [Traffic camouflage limitations](TRAFFIC_CAMOUFLAGE_LIMITS.md)
- [Test plan](TESTING.md) · [Fail-closed device testing](ANDROID_FAIL_CLOSED_TEST_PLAN.md)
- [Network compatibility](NETWORK_COMPATIBILITY.md) · [QUIC DATAGRAM proposal — not implemented](QUIC_DATAGRAM_DESIGN.md)

## 4. Release maintainers

- [Current beta.4.3 status](BETA4_3_STATUS.md) · [Release process](RELEASE_ENGINEERING_BETA43.md)
- [Release checklist](../RELEASE_CHECKLIST.md) · [APK signing](ANDROID_RELEASE_ENGINEERING.md)
- [Documentation audit](BETA4_3_DOCUMENTATION_AUDIT.md)

**Note:** documents whose titles contain a previous beta number or a historical date are retained to explain past releases, not to override the current checklist. Never assume a service on a VPS has been upgraded merely because the repository or panel displays beta.4.3.
