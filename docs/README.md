# MBox documentation — 1.0.0-beta.4.4

[Русский](ru/README.md) · [Home](../README.md)

**Current guides** (older beta.4.x status/roadmap pages are historical evidence and must not be taken as current implementation claims):

1. **Users:** [Install Android](GETTING_STARTED.md) · [Profiles and subscriptions](ANDROID_MULTI_SUBSCRIPTIONS.md) · [Release notes](../RELEASE_NOTES.md).
2. **Operators:** [First installation](SERVER_QUICKSTART.md) · [Admin](ADMIN.md) · [Subscriptions and country exits](MBOX_SUBSCRIPTIONS.md) · [REG.RU FastCGI risk](REG_RU_SHARED_HOSTING.md) · [Operations](BETA44_OPERATIONS.md).
3. **Architecture:** [End-to-end paths](ARCHITECTURE.md) · [On-demand QUIC DATAGRAM](BETA44_DATAGRAM_EXPERIMENT.md) · [Safety](SECURITY.md) · [Network compatibility](NETWORK_COMPATIBILITY.md).
4. **Release:** [Release checklist](../RELEASE_CHECKLIST.md) · [Testing](TESTING.md) · [Release body draft](GITHUB_RELEASE_BODY.md) · [GitHub Issues](https://github.com/RegSirius06/mbox/issues).

**Compatibility:** subscriptions v1 (beta.4.3) and v2 (beta.4.4 with country profiles) are separate from authenticated Noise MBD1/MBD2 OPEN payloads and record version 2. Legacy default routing remains intentional; no promise for unsupported/unsafe older clients.

**Known open risks:** REG.RU CPU/worker quotas, Android power/long idle, real physical leak/Lockdown audit and post-publication update alert validation. These are not solved simply by passing unit tests.
