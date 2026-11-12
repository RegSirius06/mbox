# MBox platform support and roadmap

<!-- MBOX-BETA4-RELEASE-STATUS -->
> **Beta 4 source candidate:** application UDP now runs through authenticated MBox frames over HTTPS/TCP or direct native QUIC/HTTP3 streams. Android QUIC endpoint and its public TLS trust are **per imported profile**, never APK-wide assets. Auto performs reconnect failover, not multipath. Signed release APK/device tests and physical leak verification remain outstanding. Read [current beta 4 status](BETA4_RELEASE_STATUS.md).


[Русский](ru/PLATFORM_ROADMAP.md)

## Today

- **Android (arm64):** the integrated `VpnService`/TUN application is implemented and field-tested in the beta 4 development branch. Native QUIC is still packaged only in its debug variant until release-build integration is completed.
- **Linux:** MBox origin/relay and Go components exist, but there is **no officially packaged desktop full-device VPN client**.
- **Windows / macOS / iOS:** no supported end-user full-device MBox VPN package at this time.

Native QUIC over UDP extends MBox beyond access-restricted mobile networks. It is potentially useful as a conventional VPN outer transport on any platform with working UDP connectivity. However, this does not mean that compiling the Android APK for desktop is possible or that Linux/Windows support is already complete.

## Minimal engineering route (subject to testing)

1. Preserve the existing Go Noise/MBox framing, flow control, session, UDP forwarding and QUIC gateway; avoid a parallel implementation of the protocol.
2. Define a small platform-facing interface for encrypted transport, socket protection/routing, DNS and TUN I/O. Reuse the Go core as far as the current process architecture permits.
3. **Linux first:** TUN device + routes/DNS + minimal executable/CLI, starting with manual HTTPS/QUIC selection. Test teardown and leak behavior.
4. **Windows next:** use an appropriate supported TUN driver/integration and configure routing and DNS. Require signed installers/drivers as applicable.
5. **macOS/iOS later:** evaluate the necessary Apple Network Extension permissions and distribution rules. Do not promise availability before checking those constraints.
6. Restore Auto failover and comprehensive leak protections separately on each platform. Android `VpnService.protect()` and Cronet integration do **not** port unchanged.

This is a roadmap, not a commitment or release schedule. Cross-platform contributions are welcome if they reuse the core and include platform-specific networking tests.
