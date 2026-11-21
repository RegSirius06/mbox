# Beta.4.4 transport stage 1 — development, not a release

[Русский](ru/BETA44_TRANSPORT_STAGE1.md)

This stage was prepared against `a580411f7f3750c40e97589ab7fadae89b04d160` (beta.4.3). It changes the Android client and the native gateway's **client** command. No server, relay, profile, Noise or database changes are required for the candidate. It does not change `VERSION`, the Android application ID, versionCode or signing certificate; those must be handled at release integration.

## Auto transport

After a QUIC failure the existing HTTPS fallback remains immediate. During a healthy HTTPS session, the supervisor starts at most one bounded native process at a time. It makes a TLS-verified HTTP/3 `HEAD /` request to the configured QUIC gateway: the beta.4.3 gateway returns HTTP 404 before forwarding or opening a user session. The result proves only reachability of that gateway over authenticated TLS/HTTP3, not an authenticated MBox origin session. Two positive checks, with at least 15 seconds between them, trigger a controlled core replacement. The normal Noise handshake and authenticated remote check remain required. A failed real QUIC connection uses the existing immediate HTTPS fallback. Application connections may break during the transition; seamless stream migration is not implemented.

Initial interval 60 seconds, maximum 900 seconds, exponential factor two, jitter up to ±20% within interval bounds. Backoff is reset on network generation changes or after 120 seconds of stable QUIC. No standby probes run while QUIC is healthy or in forced modes. Each native probe is bounded by a six-second context and a nine-second supervisor deadline before cleanup; scheduling is subject to CPU availability. The supervisor reuses its existing watch; no wake alarms or new persistent polling loops. Doze suppresses new probes. This is not a measured power-saving claim.

## Advanced settings

Seven taps on MBox > Diagnostics > Transport settings. Changes apply at the next connection; stop the VPN first. AUTO, forced HTTPS and forced QUIC, failback enable/disable, fixed/exponential backoff, bounded initial interval (30–300 seconds) and maximum (initial–1800 seconds). Defaults can be restored in the dialog before saving. Release-safe preferences are separate from old debug laboratory flags. Disabling failback retains the old 60-second reconnect cooldown, rather than pretending the new intervals are still in use. Forced QUIC without configured QUIC fails closed; switch the selection to Auto/HTTPS if necessary.

Diagnostics include actual attempts, failures, promotion requests, last HTTP/3 success and estimated due time from the monotonic policy. A promotion request is not a successful switch. There are no new RTT/jitter/loss or core traffic counters. The due time is not a scheduled Android alarm. Manual immediate probing, third-party network diagnostics and QUIC DATAGRAM are not implemented in this stage.

## Validation still required

Run the full source CI and the native HTTP/3 integration test. Rebuild both native client binaries for Android using the existing build script; a stale `libmboxquic.so` does not support `--probe`. Test on an isolated device and staging endpoints, including beta.4.3 gateway, frequent network changes, process termination, idle, Always-on/Lockdown, certificate failures and physical packet capture. Compare battery, CPU time and served traffic in the proposed 6–24 hour scenarios. This is not an independent leak audit or completed release acceptance. No production deployment or release publication is part of this stage.

### Diagnostics spacing (combined patch)

Android 15+ Activity pages account for system bars, display cutouts and the
keyboard. Diagnostics adds 28dp outer top/side padding, more space before the
card, and 18dp below it. Other pages keep their existing visual padding.
Verify on a real device in both themes and in portrait/landscape; source
regression guards are not proof of rendered layout.

### AUTO initial transport and status polish

The hidden **Start AUTO with HTTPS** setting is off by default. It applies once per new VPN supervisor run, not on promotion or network change. A successful initial HTTPS session arms the configured initial interval; two successful HTTP/3 checks are still required before controlled promotion. If the initial HTTPS connection cannot authenticate, the next AUTO attempt can choose QUIC. Forced modes take priority. Disabling failback suppresses background promotion. Verified connection status is owned by the supervisor; later core startup/heartbeat log messages cannot regress it. Full Android runtime validation remains required.
