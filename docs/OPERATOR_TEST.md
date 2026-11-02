# Controlled mobile-operator test: `.fun` vs `.ru`

[Русская версия](ru/OPERATOR_TEST.md)

## Goal

Localize the failure layer instead of guessing about DPI. The same backend should be exposed through two hostnames, ideally `.fun` and `.ru`, so hostname/category treatment can be separated from IP/backend/protocol behavior.

## Current field status

The current Cronet branch has not yet been tested during active white-list restrictions. Historical observation: the `.fun` frontend was reachable in the relevant restricted mode on MTS/T2 but could fail in a normal browser on MegaFon. MegaFon therefore remains unresolved.

## Test setup

Keep these as identical as practical:

- REG.RU shared-host account and webroot;
- A/AAAA/backend addresses;
- relay binary/config;
- MBox origin;
- API paths;
- certificate quality/validity;
- test time and phone/APN.

Use a `.ru` domain only as a controlled variable. Since 2026-09-01 `.ru/.рф/.su` administration has additional identity requirements in Russia; complete the registrar/provider identification normally.

## Tests per network

For Wi-Fi, MTS, T2 and MegaFon, record:

1. Chrome -> `.fun`;
2. Chrome -> `.ru`;
3. MBox Cronet -> `.fun`;
4. MBox Cronet -> `.ru`;
5. MBox Go diagnostic stack -> `.fun`;
6. MBox Go diagnostic stack -> `.ru`;
7. credential-free `mbox-netprobe` where practical.

Use the same device and keep only one variable changed at a time.

Example probe:

```bash
GO111MODULE=off go build -o /tmp/mbox-netprobe ./tools/mbox-netprobe
/tmp/mbox-netprobe -json https://FUN_HOST/ https://RU_HOST/ | tee probe.jsonl
```

## Result matrix

| Network | Chrome .fun | Chrome .ru | Cronet .fun | Cronet .ru | Go .fun | Go .ru |
|---|---|---|---|---|---|---|
| Wi-Fi | | | | | | |
| MTS | | | | | | |
| T2 | | | | | | |
| MegaFon | | | | | | |

## Interpretation

- both hostnames fail at TCP -> IP/subnet/routing/allow-list suspicion;
- TCP works but TLS fails -> SNI/ClientHello/certificate/TLS policy;
- TLS works but HTTP fails -> HTTP/frontend filtering;
- Chrome works but Go-MBox fails and Cronet-MBox works -> public network-stack fingerprint is material;
- `.ru` works while `.fun` fails on the same backend -> hostname/SNI/category treatment becomes the leading hypothesis;
- HTTP reaches MBox but OPEN/Noise fails -> internal MBox/auth/protocol issue, not carrier reachability.

Do not infer a universal carrier rule from one SIM/region/time window.
