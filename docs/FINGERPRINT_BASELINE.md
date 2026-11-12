# TLS/HTTP fingerprint baseline and Android Cronet transport

<!-- MBOX-BETA4-CURRENT-DOCS -->
> **Version context:** Some details below describe earlier beta.2/beta.3 implementations or historical observations. In **beta 4**, native QUIC/HTTP3 and application UDP are supported, and HTTPS/Cronet is the fallback. See [current Android guide](ANDROID_BETA4.md), [transport model](BETA_4_TRANSPORTS.md), and [security limits](SECURITY.md). The signed APK has been built and installed, but comprehensive physical leak testing remains incomplete.




[Русская версия](ru/FINGERPRINT_BASELINE.md)

## Current architecture

The released beta.2 Android path exposed Go `crypto/tls` + `net/http` directly to the public frontend. The current branch instead uses:

```text
Go core: Noise / MUX / replay / ACK
              |
              | authenticated loopback bridge
              v
Android Java: bundled Chromium Cronet
              |
              v
DNS / TCP / TLS / ALPN / HTTP/2 -> public shared-host frontend
```

Cronet is the Chromium networking stack exposed to Android applications. HTTP/2 is enabled; QUIC is disabled in the first rollout so operator tests do not introduce a second major transport variable.

## Why this is better than UA spoofing

Changing only `User-Agent` would leave a Go TLS ClientHello, Go connection behavior and Go HTTP implementation underneath a browser-looking header. MBox instead delegates the complete outer HTTPS transaction to one Chromium-derived stack. This substantially reduces the obvious implementation mismatch.

It is still not an exact-browser claim. Cronet can differ from Chrome in process context, configuration, extensions, HTTP/2 settings, caching, request headers, timing and version.

## Real-device confirmation

On 2026-11-02 the development APK was built and installed on a real ARM64 Android device. Runtime logs confirmed:

```text
NET stage=DNS code=DNS_DELEGATED stack=cronet
NET stage=HTTP code=HTTP_STACK stack=cronet ...
NET stage=HTTP code=HTTP_OK stack=cronet proto=HTTP/2.0 status=200 ... alpn=h2
NET stage=MBOX_OPEN code=MBOX_OPEN_OK
NET stage=NOISE code=NOISE_OK
NET stage=TUNNEL code=TUNNEL_ESTABLISHED
```

Telegram, Instagram and video traffic also worked through the tunnel. This proves Cronet is on the live outer path; it does **not** prove equality to Chrome's JA4/HTTP2/browser fingerprint.

## Baseline capture still required

On the same Android device, network and endpoint compare:

- Chrome;
- MBox Cronet mode;
- MBox Go diagnostic mode.

Compare, where observable at a controlled endpoint:

- TLS ClientHello/version/cipher/extensions/order/groups/signature algorithms;
- SNI and ALPN;
- HTTP protocol and HTTP/2 settings/behavior;
- connection reuse/resumption;
- request sizes and timing.

HTTP/2 SETTINGS are encrypted on the public wire after TLS, so a meaningful comparison may require endpoint instrumentation/key logging rather than only a passive pcap.

## Runtime regression check

Capture Android logs and run:

```bash
python tools/mbox-fingerprint-check.py --require-h2 mbox-cronet.log
```

The checker should report Cronet selected, Go public stack absent, HTTP/2 present, Noise established and tunnel established.

## Controlled timing/size baseline

For an A/B capture, issue a temporary profile with:

```bash
sudo mboxctl user link USER \
  --android-http-stack cronet \
  --network-debug \
  --deterministic-transport
```

`deterministic_transport` fixes camouflage timing/batch/padding-length/chunk-boundary choices only. It does not disable Noise ephemeral randomness, nonces, key generation or random padding bytes. Do not use deterministic mode as the production default.
