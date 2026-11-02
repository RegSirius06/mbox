# Code layout

[Русская версия](ru/CODE_LAYOUT.md)

MBox keeps large responsibilities in separate source files while retaining the existing Go packages and wire protocol.

## Client

- `main.go` — process bootstrap and SOCKS accept loop only.
- `types.go` — protocol/session state types and frame constants.
- `session.go` — tunnel creation and terminal failure handling.
- `http_transport.go` — MBox HTTP envelope/request helpers.
- `uplink.go` / `downlink.go` — directional transport scheduling and reliability.
- `streams.go` — MUX stream lifecycle and frame delivery.
- `camouflage.go` — timing, batching and padding configuration.
- `network.go` — DNS/address selection, Happy-Eyeballs-style dialing, TLS/HTTP diagnostics.
- `android_bridge.go` — authenticated localhost bridge client for Android Cronet.
- `crypto.go` — Noise transport state only.
- `flow.go` — bounded queues and flow control primitives.

## Server

- `main.go` — process bootstrap, route registration and server startup.
- `types.go` — session/stream/runtime state types and limits.
- `session.go` — session registration, policy enforcement and cleanup.
- `streams.go` — MUX stream lifecycle.
- `downlink.go` — DOWN queue batch accounting.
- `http_transport.go` — HTTP envelopes, ACK/replay and DOWN response commit logic.
- `camouflage.go` — external HTTP camouflage/padding configuration.
- `status.go` — runtime status snapshot.
- `handshake_logging.go` / `handshake_limit.go` — pre-auth protection and diagnostics.
- `cover.go` — cover-traffic engine.
- `crypto.go` — Noise handshake/record state.
- `flow.go` — bounded queues and flow-control primitives.

The split is intentionally package-preserving: declarations moved between files, but package boundaries and protocol semantics did not change.

## Android

- `MBoxVpnService.java` — VpnService lifecycle/supervisor and Go/HEV process ownership.
- `CronetBridge.java` — authenticated loopback bridge and Chromium Cronet requests/metrics.
- `UnderlyingNetworkMonitor.java` — `INTERNET + NOT_VPN` physical-network selection/debounce.
- `ProfileStore.java` — profile generations, provisioning import and stable per-installation device ID.

Android source files are also guarded by the source-layout CI limit so lifecycle/networking code does not collapse back into one unbounded service class.
