# QUIC DATAGRAM in 1.0.0-beta.4.4: implemented, experimental cryptography

[Русский](ru/BETA44_DATAGRAM_EXPERIMENT.md) · [Architecture](ARCHITECTURE.md) · [Testing](TESTING.md)

**Status:** working field-trial feature, **not** a proposed staging-only lab and not independently audited. A separate authenticated HTTP/3 DATAGRAM lane carries **UDP DATA** of already-open MBox UDP associations. TCP, Noise OPEN, UDP association OPEN/CLOSE, authenticated control and HTTPS remain on reliable streams. Native QUIC and origin communicate over private Unix sockets; REG.RU does not carry these DATAGRAM frames.

A lane is created **only for real UDP traffic**, not merely when a UDP association opens. It uses authenticated attach/accept proofs, encrypted directional datagram records, replay protection and bounded queues. When an idle QUIC datagram connection expires (observed ~30–33 seconds), new UDP traffic uses streams while a new lane attaches. Each successful reattach uses a monotonically increasing authenticated epoch and independently derived AEAD key/nonce domain, preventing reuse of AES-GCM counters. No periodic QUIC PING or background poll is scheduled for this lane. Packet drops are possible from oversize datagrams, unavailable network, setup, MTU and bounded queue pressure; selected packets are not retransmitted through a second path to avoid duplicates.

**Hidden developer UDP routing** (seven taps on MBox title → Diagnostics): `AUTO` (default), `STREAMS only` (disable datagrams), `DATAGRAM only` (no UDP DATA fallback at either endpoint, may drop while the lane is down; HTTPS has no such datagram lane). Changes apply on next VPN connection. TCP and control remain reliable in every mode. The default is the recommended battery-conscious choice.

**Compatibility:** older beta.4.3 clients use conventional reliable MBox streams. The first datagram lane attach preserves the old epoch-0 format; later attaches require rekey-capable beta.4.4 origin and gateway support. Noise/record versions and HTTP subscription versions are separate. The origin must permit the Unix socket and ACL for the gateway user. No provider-wide guarantee of device battery life or physical leak freedom.

**Field evidence 2026-11-21:** Android logs show first ACTIVE → idle fallback after ~30s → new ACTIVE on later deliberate UDP with no Noise session restart. Wi-Fi/mobile handover, airplane mode and screen-off recovery were operator-tested. No controlled current/power measurement, 24h soak, independent crypto review or full Lockdown/packet-capture audit is claimed.

Check: `GO111MODULE=off go test -race ./common/dgram ./common/noise ./client ./server ./relay`; `(cd native-quic && go vet ./... && go test -race -count=1 ./...)`; `python3 -m pytest -q tests`; `python3 tools/check-source-layout.py`. Recheck actual signed APK; not just debug.
