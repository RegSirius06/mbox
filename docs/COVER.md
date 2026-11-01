# Server cover traffic

MBox includes an optional server-originated cover traffic engine.

## Goal

Keep the public shared-host frontend from having a simple history of “almost completely idle until a real VPN client appears”. The MBox origin/server generates irregular HTTPS activity through the public frontend only while no real MBox session is active.

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

A successful authenticated MBox OPEN immediately cancels the current cover context. The cover loop remains suppressed while at least one session has recent authenticated activity. A clean session close makes the server idle immediately; an unclean network disappearance is handled by `idle_after_ms`.

## Traffic model

The engine chooses weighted states:

- `quiet`: no requests for 1–20 minutes;
- `casual`: 1–5 small page-like groups;
- `active`: 3–12 larger groups;
- `burst`: 30–120 resource requests with short randomized gaps.

Requests use varied asset-like paths/extensions and browser-like headers. Responses vary by resource type and size. This is intentionally stochastic rather than periodic.

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

A normal client shutdown sends a best-effort `POST` to the configured `close_path` before the process exits. This lets the server remove the live session immediately and begin the randomized cover start grace without waiting for `idle_after_ms`. If the client disappears because of a network loss or crash, no close request can be guaranteed; the server then falls back to the idle timeout.
