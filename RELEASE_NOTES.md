# MBox v1.0.0-beta.4.4 — prerelease notes

[Русский](RELEASE_NOTES.ru.md) · [Home](README.md) · [Checklist](RELEASE_CHECKLIST.md)

**Android 10+, ARM64, `io.mbox.android`, versionCode 11. Experimental prerelease, not RC1.** This document is prepared before publishing; verify final artifact and commit first.

## Highlights

- **True QUIC DATAGRAM for application UDP** on a separately authenticated HTTP/3 lane; independent per-epoch AEAD key derivation and replay protection, bounded queues, graceful streams fallback. No continuous DATAGRAM PINGs. On real UDP traffic an idle channel can reattach without a new Noise session. Do not confuse application-UDP-over-MBox with forwarding TCP by DATAGRAM.
- **Hidden developer UDP mode selector:** AUTO (default), STREAMS only, DATAGRAM only. The last mode deliberately drops UDP instead of silently falling back; TCP/control remains reliable. No API or setting exposed on the normal home screen.
- **Android power and reliability work:** reduced inactive network polling and UI refreshes, deduplicated wake checks, on-demand UDP channel setup. Android screen-off, physical network loss, handover, and reconnect were operator-tested. Battery consumption parity is an observation, **not** a published controlled benchmark.
- **Transport:** QUIC direct to gateway and HTTPS/Cronet through optional FastCGI ingress; AUTO ingress fallback. Shared REG.RU frontend is unchanged by adding DATAGRAM.
- **Multiple country exits:** authenticated Noise OPEN route request (`MBD2`, `egress_id`) selects an origin-allowlisted local SOCKS5; one HTTPS subscription URL returns default plus named exits using subscription v2. For beta.4.3, v1 still returns the default route. DK and KZ were observed working on separate SOCKS5 exits.
- **Admin and UI:** hidden diagnostics in release APK, profile chooser spacing and naming, light/dark UI, SSH-only admin panel with optional explicit HTTPS, editable default display label, country exit form. GitHub Releases update alert behavior retained (user may dismiss; no bypass of the VPN).
- **Operations:** interactive install.sh --check is read-only, explicit update/fresh-install modes, signed APK and rollback workflow, REG.RU relay build/self-check and worker recycle tooling. Upgrading origin, gateway and frontend remains independently controlled.

## Compatibility and security

- Subscription **v1 vs v2** is distinct from Noise OPEN **MBD1 vs MBD2** and the internal record framing. The default route is used when no egress ID is specified; unknown requested exits must not silently become a different route. Old beta.4.3 clients were field-verified. This does not promise compatibility with arbitrary old or unauthenticated clients.
- Existing profile PSKs, subscription URLs and signer identity must remain stable. Publish a release-signed APK only; no private endpoints, subscriptions, credentials, keystores or runtime databases.
- DATAGRAM's extra crypto and reconnect scheme is experimental; there is **no independent security review**, physical-network leak certification, or guarantee of universal DPI resistance.

## Known risks and follow-up issues

1. **High — REG.RU FastCGI CPU/worker quotas and frontend availability.** Limited shared-host CPU can trigger throttling or process termination under proxy workloads; no provider CPU metrics integrated. Avoid claiming this fixed or silently deploying relay changes.
2. **Medium — Android energy:** no controlled device power comparison or long-duration soak; always-on background signals, QUIC handshakes and radio wakeups still need profiling.
3. **High validation gap — physical network/Lockdown:** IPv4/IPv6/DNS/UDP leak captures, captive/switching conditions, independent review and certificate rotation need testing.
4. **Medium — release update notifications:** beta.4.3-to-beta.4.4 link/opening needs an end-to-end post-publication test; checks are in-VPN, and notification permission can suppress UI.
5. **UX:** named/default exits share equal routing ability, but 'default' remains a distinct configuration choice for legacy clients.

## Publication requirements

CI green on exact release SHA; complete `RELEASE_CHECKLIST.md`; signed `MBox-1.0.0-beta.4.4-android-arm64.apk` plus `.sha256` with verified original certificate; GitHub tag `v1.0.0-beta.4.4` on that SHA, GitHub **Pre-release** selected; final real-device upgrade and no secrets. Real checksums cannot be filled until signing.
