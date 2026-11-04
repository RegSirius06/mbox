# Network compatibility notes

[Русская версия](ru/NETWORK_COMPATIBILITY.md)

## Historical operator observation

Before the current Cronet audit branch, the deployment's `.fun` shared-hosting frontend was observed reachable under restricted mobile Internet on MTS/T2, while on MegaFon the same `.fun` site could fail even in an ordinary browser. That is important because it places at least one MegaFon failure mode **before MBox/Noise**.

Treat this only as a historical/local observation. Carrier filtering can differ by region, APN, subscriber profile and time.

## Current branch status

On **2026-11-05**, the operator tested the updated beta.2.2 code (renamed beta.3) with active mobile Internet allow-list restrictions. Telegram and regular browsing worked through the existing REG.RU FastCGI → RU VPS path. The mobile carrier was not specified; this confirms only that individual test, **not** carrier-wide reliability or absence of packet leaks. The official beta.2.1 Android APK is incompatible with strict origin `cmac`.

MegaFon remains unknown for the current build. Do not describe it as fixed or broken until a controlled test is run.

## `.ru` hypothesis

A useful next experiment is to point `.fun` and `.ru` hostnames at the same REG.RU shared-host frontend/backend, with the same HTTP paths and as-close-as-possible TLS/server configuration. If `.ru` works on MegaFon while `.fun` fails before MBox, that is evidence for hostname/SNI/category treatment rather than an MBox protocol failure. It is a hypothesis, not a guarantee.

## What to collect when a network fails

Capture the establishment stages:

```text
DNS -> selected addresses -> TCP -> TLS/certificate -> ALPN -> HTTP -> MBox OPEN -> Noise -> tunnel
```

Use `tools/mbox-netprobe`, Android `--network-debug`, and the matrix in `docs/OPERATOR_TEST.md`. Never publish PSKs, provisioning links or full secret URLs.
