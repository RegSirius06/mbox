# Android power/idle profiling for on-demand QUIC DATAGRAM and recovery

**Severity:** medium (P2), performance validation. **Component:** Android VpnService + native QUIC + Go core.

beta.4.4 removes persistent DATAGRAM PINGs and starts a lane on real UDP; it expires at idle (~30–33s in operator logs) and reattaches on demand with a new AEAD epoch. Android supervisor polling, screen wake checks, and foreground UI refresh were reduced. The maintainer observed acceptable battery life, but no controlled power benchmark exists.

**Acceptance plan:** compare beta.4.3/beta.4.4 on the same phone, screen off/on, Wi-Fi/mobile, UDP idle vs sustained video/voice, good/poor signal, airplane mode, Lockdown, two battery conditions where possible; measure Android Battery Historian/bugreport, wakelocks, network wakeups, radio activity, QUIC handshakes, CPU, reconnect failures and overnight battery delta. Preserve fail-closed and recovery. Avoid claims of negligible drain without measurements.
