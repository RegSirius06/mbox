# Physical-device Android fail-closed test gate

This is a **manual acceptance gate**: the unit tests and ADB-only checks below
cannot establish the absence of physical egress. Do not assert P0 closure
without a packet capture covering the actual Wi-Fi/cellular interfaces.

## Prerequisites

- Dedicated test phone with MBox beta.2.1 configuration backed up.
- ADB installed on Arch; USB debugging enabled on phone.
- In Android Settings -> Network & internet -> VPN -> MBox, enable **Always-on
  VPN** and **Block connections without VPN**. The app cannot enable lockdown
  itself. Before crash tests run `android/tools/preflight-lockdown.sh`.
- Preserve working MBox server and shared-host FastCGI deployment unchanged.
- Build debug APK from the current source and inspect APK/Go core hashes.

## Non-destructive tests

1. Install test APK on the device and connect MBox.
2. Verify Telegram messages, images, regular HTTPS browsing, sustained upload
   and download, and IPv4 and IPv6-only test destinations (where reachable).
3. `adb forward tcp:19090 tcp:10890` then run
   `python3 tools/android_socks_probe.py --port 19090` on Arch. Remove the
   forward with `adb forward --remove tcp:19090` after the probe.
4. The SOCKS5 local probe MUST return `REP 07` for BIND and UDP ASSOCIATE.
   The test does not prove that ordinary app UDP cannot leak elsewhere.
5. Use an independently controlled Wi-Fi router with `tcpdump` to capture all
   phone egress. A rooted test phone or mobile operator-side capture is needed
   to observe cellular egress directly. Capture on TUN/VPN side as well where
   instrumentation allows; preserve timestamps and record the outer HTTPS
   endpoint(s) for offline classification.
6. Test DNS over UDP/TCP, STUN, QUIC, WebRTC, Telegram and IPv6 while capturing.
   All user-initiated traffic must be inside MBox HTTPS, or be blocked. Account
   for Android connectivity probes and other OS traffic explicitly rather than
   silently whitelisting them.

## Disruptive tests (ONLY after lockdown verified)

- Drop Wi-Fi (keep mobile data), restore Wi-Fi, then remove all connectivity.
- Enable/disable airplane mode.
- Run `adb shell am force-stop io.mbox.android` while capturing.
- Restart the app, kill the Go core using test instrumentation, let screen turn
  off, and test Doze/reconnect.
- At every phase: traffic should be blocked or tunneled; none should bypass.
- Log failure time, environment, packet traces and correlating MBox events.

## Evidence and decision

Pass requires Android lockdown enabled, verified refusal of unsupported SOCKS5
commands, successful reconnect on handover, **and physical captures with zero
unexplained direct user-payload egress**. If any of these is absent, status
remains `P0 NOT PROVEN` despite functioning Telegram/HTTPS.
