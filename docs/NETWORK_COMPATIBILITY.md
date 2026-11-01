# Network compatibility notes

This file records field observations, not universal guarantees. Mobile-carrier filtering can vary by region, APN, subscriber profile, time and DPI policy.

## 2026-11-02 field test

Tested with MBox `1.0.0-beta.2` using the Noise-only wire generation.

- **Wi-Fi:** worked as the unrestricted baseline. White-list mode was **not** active on this Wi-Fi network.
- **MTS mobile network:** MBox connected and carried traffic while the operator's white-list restrictions were active in this test.
- **MegaFon mobile network:** MBox did **not** establish a usable bypass while white-list restrictions were active in this test.

Do not generalize this single field result to every MTS/MegaFon region or APN. Treat MegaFon white-list mode as a known beta.2 compatibility issue until reproduced and investigated with packet captures and transport logs.

## What to collect when a network fails

Record the following without publishing credentials or provisioning URLs:

```text
MBox version
Android version/device
carrier + region + APN
whether white-list restrictions are actually active
whether the session OPEN reaches the origin server
HTTP status returned by the shared-host relay
client reconnect/error log
origin mbox-server log for the same timestamp
```

For packet-level research, capture only what is needed and redact public IPs/identifiers before publishing. Never publish PSKs, `mbox://` links, `/etc/mbox`, `users.db`, server private keys or backup archives.
