# Beta.3 qualification and trust boundaries (2026-11-05)

[Русская версия](BETA_3_RELEASE_STATUS.ru.md)

**Version:** `1.0.0-beta.3` / tag `v1.0.0-beta.3` / Android `versionCode=6`.
**Classification:** experimental, potentially suitable for a **limited, informed test group**, not certified production VPN service and not completion of the 55-section hardening specification.

## One verified operator deployment

- Origin binaries were installed on a **root-managed RU VPS** from Arch; rollback backup, isolated restore test, `mboxctl doctor`, and running-process SHA-256 verification succeeded.
- Updated Android APK connected over the existing **REG.RU shared hosting FastCGI** frontend; Telegram and browser loaded during an active mobile allow-list restriction. Device VPN reportedly resumed after reboot.
- Origin now requires `auth.require_authenticated_control=true`; with this setting the **officially published beta.2.1 APK is incompatible**, while the updated `cmac` client worked. Noise ciphertext records and provisioned PSK profile schema stayed the same.
- A beta.2.2 development APK with `cmac` was tested. Renaming to beta.3 changes version strings and Android versionCode only; run device smoke tests again on a beta.3-built APK before publishing that artifact.

## Security boundary and threats

| Observer | What it can see or do | What is not guaranteed |
|---|---|---|
| ISP/DPI between phone and REG.RU | REG.RU destination IP, outer TLS/client/network fingerprint, traffic timing and packet sizes | Indistinguishability from Chrome, indefinite allow-list accessibility, encrypted DNS behavior outside MBox without capture |
| REG.RU frontend | HTTP paths/headers/query including `seq`, `ack`, `max`, `cmac`, request timing, ciphertext; it can drop/reorder/replay HTTP requests | It must not learn plaintext/PSK, but `cmac` does not hide HTTP metadata and relay compromise can deny service |
| MBox origin | Authenticated clients, decrypted SOCKS destination/traffic before onward routing | End-to-end secrecy from origin operator |
| Android physical interface | Explicit MBox Cronet HTTPS outer transport; Android OS may generate independent network traffic | No leak-free assertion until full Wi-Fi/LTE capture and process-failure tests |

## Gates before broader public distribution

1. **P0 leak testing:** capture Wi-Fi router + cellular where technically possible, including DNS UDP/TCP, QUIC, STUN, generic UDP, IPv4, IPv6, forced stop, crash, Lockdown and Wi-Fi/LTE switches. Unsupported UDP must be blocked, never silently sent direct.
2. **P0 compatibility:** compare released beta.2.1 rejection against strict `cmac`; run beta.3 APK smoke, service reboot and Android Always-on/Lockdown.
3. **P1 stability:** 10/50/100 concurrent clients, relay slot saturation, HTTP 429/503 recovery, reconnect storm, bounded memory/FD/goroutines and multi-day soak.
4. **P1 release:** non-debug signing key outside repository, pinned signing-certificate digest, manifest and signature gate, signed APK hash, reproducible release metadata, CI pass, secret scan.
5. **P2 threat model:** fingerprint captures Chrome vs Cronet vs Go; analyze polling/padding/cover scheduling and resource budgets. Provider policy/permission for proxy usage on shared hosting must be checked.

## TCP/UDP policy

TCP travels inside authenticated Noise transport. Local SOCKS5 `BIND` and `UDP ASSOCIATE` are rejected; UDP applications may not work or may choose their own TCP fallback. Native UDP over MBox is a future protocol feature. Android OS **Always-on VPN + Block connections without VPN** is required for fail-closed behavior across process death; app code alone cannot enforce OS lockdown.

## Release decision

Publishing `v1.0.0-beta.3` as a clearly labeled **experimental GitHub prerelease** is an engineering decision distinct from certifying safety for widespread end users. Without packet-capture evidence, describe it as early access and do not state that it is leak-free or reliably bypasses every mobile allow-list. Preserve the 55-section gap matrix as `BETA_2_2_HARDENING_MATRIX_RU.md` (historical snapshot).
