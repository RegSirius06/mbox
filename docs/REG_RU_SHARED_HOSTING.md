# REG.RU shared-hosting frontend guide

[Русская версия](ru/REG_RU_SHARED_HOSTING.md)

## What this project expects

The deployment tested by MBox uses **virtual/shared hosting at REG.RU** as the public HTTPS frontend. The FastCGI relay and ordinary decoy website live there; the MBox origin/server can be a separate VPS in Russia or elsewhere, and the final Internet exit can be another hop again.

```text
Android -> HTTPS -> REG.RU shared hosting/FastCGI -> MBox origin -> sing-box -> exit
```

Do not confuse this with a REG.RU VPS. The value of this topology is the ordinary shared-web-hosting frontend and its network placement.

## Plan selection

REG.RU currently separates Linux shared hosting into speed, economy and VIP groups. For MBox use a normal **speed-class** shared-hosting plan (`Хост-0`, `Хост-1`, `Хост-3` in current REG.RU documentation) rather than the smallest static-page-only economy plan. The reason is not storage: the relay benefits from normal dynamic/FastCGI-capable CPU/I/O headroom and predictable web-host behavior.

REG.RU currently advertises unlimited traffic on shared-hosting plans. Treat that as provider-plan semantics, not as a mathematical/no-policy limit: their contract allows intervention when a nominally unlimited resource creates abnormal load or violates service rules. Unlike many CDN products, the plan is not presented as a simple per-GB egress allowance, but you must still obey current fair-use and acceptable-use rules.

**Provider policy matters.** REG.RU help materials list proxy services among disallowed uses on shared hosting. This repository documents a technically tested frontend topology; it does not grant permission to violate provider terms. Before deployment, verify that your intended use is permitted by the current contract/support policy.

## Identity verification

Hosting customers can be required to complete provider identity verification. Complete it with accurate data. For `.ru/.рф/.su`, Russian rules effective from 2026-09-01 add mandatory administrator identification through ESIA for registration/renewal.

A domain does not have to be registered at REG.RU merely to point at REG.RU shared hosting, but provider/registry identity rules still apply.

## Domain choice and the MegaFon hypothesis

A `.fun` domain has historically worked in the relevant restricted mode on MTS/T2 in this deployment class, while on MegaFon the `.fun` site itself has at times failed in an ordinary browser. That means at least one MegaFon failure mode can occur before MBox.

The next useful test is therefore **not** to change Noise or invent more obfuscation. Put `.fun` and `.ru` hostnames on the same shared-host backend and compare them. A `.ru` name may be treated differently by an operator allow-list/category system, but that is only a hypothesis until measured.

Keep as much as possible identical:

- same shared-host account/webroot/backend;
- same relay;
- same origin;
- same MBox paths;
- same phone/APN/time window;
- valid certificates for both hostnames.

Then run the matrix in [`OPERATOR_TEST.md`](OPERATOR_TEST.md).

## Deploy the relay

Generate the bundle on the MBox admin/origin host:

```bash
sudo mboxctl relay bundle
sudo mboxctl relay paths
```

Deploy over SSH/SCP when available:

```bash
sudo mboxctl relay deploy \
  --ssh USER@HOST \
  --remote-dir /var/www/USER/data/www/front.example.com
```

Or use FTPS:

```bash
sudo mboxctl relay deploy \
  --ftp-host HOST \
  --ftp-user USER \
  --remote-dir /path/to/webroot
```

Normal deployment updates the relay files without overwriting an existing site. Add `--site` only when intentionally installing the packaged decoy `index.html`/`404.html`. Preserve a known-working FastCGI/`.htaccess` mapping: shared-host activation details vary between accounts.

## Verify the frontend

```bash
sudo mboxctl relay test
```

An unauthenticated transport probe normally receives an ordinary 404. For operator diagnosis use `tools/mbox-netprobe` and the stage diagnostics rather than interpreting one browser error as proof of the failure layer.

## Current white-list qualification

**MBox 1.0.0-beta.2.1 has not yet been field-tested during active white-list restrictions**. The frontend/FastCGI topology and beta.2 MBox wire semantics are preserved, so no regression is currently expected where the older deployment already worked, but this is not a fresh guarantee. MegaFon remains unknown until the controlled `.fun`/`.ru` test is performed.

## Provider references checked for this guide

- REG.RU shared hosting / FAQ: https://reg-panel.reg.ru/hosting/shared
- REG.RU plan-selection guide: https://help.reg.ru/support/hosting/zakaz-hostinga-rabota-s-uslugoy/sovety-po-vyboru-tarifa-hostinga
- REG.RU identity procedure: https://help.reg.ru/support/pravovyye-voprosy/personalnyye-dannyye/protsedura-identifikatsii-personalnykh-dannykh

Provider plans and terms change; re-check them before deployment.
