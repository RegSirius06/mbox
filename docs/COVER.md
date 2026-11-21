# Server cover traffic

<!-- MBOX-BETA4-CURRENT-DOCS -->
> **Version context:** Some details below describe earlier beta.2/beta.3 implementations or historical observations. In **beta 4**, native QUIC/HTTP3 and application UDP are supported, and HTTPS/Cronet is the fallback. See [current Android guide](ANDROID_BETA4.md), [transport model](BETA_4_TRANSPORTS.md), and [security limits](SECURITY.md). The signed APK has been built and installed, but comprehensive physical leak testing remains incomplete.


[Русская версия](ru/COVER.md)

MBox includes an optional server-originated cover traffic engine.

## Goal

Keep the public shared-host frontend from having a simple history of “almost completely idle until a real VPN client appears”. The MBox origin generates irregular HTTPS activity through the public frontend only while no authenticated MBox session has recent activity. This costs FastCGI CPU when the frontend forwards cover requests.

## Path

```text
mbox-server (RU)
  -> public HTTPS frontend
  -> REG.RU FastCGI relay
  -> origin HTTPS / Caddy
  -> mbox-server `/api/v1/assets/*`
```

No second `mbox-client` is required on the server.

## Suppression by real users

A successful authenticated MBox OPEN cancels an in-flight cover profile; the cover loop checks active sessions at least once per second and enters `suppressed`. No new cover requests are started while a session has authenticated activity within `idle_after_ms`. A clean session close removes it from the active set; an unclean network disappearance ages out after `idle_after_ms`. A request already in flight at OPEN may finish/cancel. A fresh randomized start grace applies when cover resumes.

**Trade-off:** suppressing cover reduces shared-host CPU use but produces a traffic-rate correlation with real session activity. Do not describe this as indistinguishable browser traffic or complete DPI resistance.

## Traffic model

The engine chooses weighted states:

- `quiet`: no requests for 1–20 minutes;
- `casual`: 1–5 small page-like groups;
- `active`: 3–12 larger groups;
- `burst`: 30–120 resource requests with short randomized gaps.

Requests use varied asset-like paths/extensions and Go HTTP-client headers (not a browser fingerprint). Responses vary by resource type and size. This is intentionally stochastic rather than periodic.

## Safety limits

`max_rps` and `max_requests_per_hour` are hard attempt budgets. Failed requests count toward the rate/hour budget. Defaults are conservative for shared hosting.

## CLI

```bash
mboxctl cover simulate --hours 24
mboxctl cover test
mboxctl cover enable
mboxctl cover status
mboxctl cover disable
```

## Scope

This cover traffic changes what the shared frontend observes. It does not create traffic on an offline client access network and is not a substitute for client-side timing camouflage.

## Graceful client disconnect

A normal client shutdown sends a best-effort `POST` to the configured `close_path` before the process exits. This lets the server remove the live session immediately and begin the randomized cover start grace without waiting for `idle_after_ms`. If the client disappears because of a network loss or crash, no close request can be guaranteed. A reconnecting beta.2.1 Android client can supersede the stale session immediately using its authenticated `device_id`; otherwise the generic idle timeout remains the fallback.
