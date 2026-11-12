# Contributing to MBox

[Русский](CONTRIBUTING.ru.md)

MBox is experimental networking software. Bug reports, focused fixes, documentation corrections and cross-platform experiments are welcome. A proposed change may be reviewed, modified or declined; opening a PR does not guarantee merge.

## Report defects

Open [GitHub Issues](https://github.com/RegSirius06/mbox/issues) using **Bug report**. First check for existing reports. Provide steps to reproduce; expected/actual behavior; Android/MBox version; HTTPS, QUIC or Auto mode; Wi-Fi/LTE/carrier/roaming; whether access restrictions are active; time and time zone; and redacted logs. An intermittent bug is still worth reporting even without a deterministic reproducer.

**Protect secrets.** Never publish full `mbox://` links, credentials, PSKs, HMAC keys, private keys, auth headers, private frontend paths, raw user databases or unredacted logcat. For an exploitable security defect, check whether GitHub private vulnerability reporting is enabled before disclosing details in public.

## Send code via Pull Request

1. Fork the repository and create a focused branch. Use the smallest correct implementation; reuse existing Go packages and avoid speculative abstractions.
2. Include a regression test for any bug fix; state which transport and platform were exercised. For network code, cover failures, cancellation and resource cleanup.
3. Run relevant Go tests (`-race` where possible), Python tests and Android build/contract tests. Do not claim tests you did not run.
4. Include updated Russian and English documentation when changing behavior. Avoid embedding real service domains, credentials or private configuration in tests.
5. Open a Pull Request explaining the bug, approach, test results, risks and platform requirements. Preserve the existing issue context where possible.

For platform work, see [roadmap](docs/PLATFORM_ROADMAP.md). Native QUIC's Go core is reusable, but desktop VPN network integration must be implemented and tested separately.
