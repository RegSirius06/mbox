# Android: multiple MBox subscriptions (beta.4.2)

This change adds local support for **up to 12 independent HTTPS subscriptions**, each providing **1–12 MBox ingress profiles**. It does not change the MBox wire protocol, server Noise identity or direct `mbox://` provisioning.

## Behavior

Open **Settings → Add HTTPS subscription**. Paste a subscription URL (e.g. `https://your-host/api/v2/<token>`). The app downloads JSON `version=1`, validates all `profiles[].link` entries as regular MBox profiles and activates its first available ingress. **Settings → Subscriptions** opens a list of imported subscriptions. Select one to use it, select an ingress/endpoint, update that subscription or remove its local copy. **Refresh all subscriptions** fetches each independently. Failures do not remove cached profiles; successful refreshes use the same Noise identity pinned on first import. The endpoint list includes any configured shared-hosting relays plus QUIC metadata embedded in the MBox profile; QUIC vs HTTPS remains automatic in release builds.

Subscription URLs, profile JSON and PSKs are encrypted by the existing Android Keystore AES-GCM vault. Only random record IDs and selection IDs live in preferences. The existing single-subscription storage remains readable under a virtual `legacy` record; no migration rewrites it. New records live under the app-private `subscriptions/items/<random-id>/` directory.

Importing a **new direct `mbox://` link** stores its link encrypted and makes it independently selectable via **Use direct profile**. A direct profile imported by an *older* APK continues to work after APK upgrade, but cannot be reselected after switching to another subscription unless its original link is imported once under the new APK. Preserve the original profile link. Import/selection requires a stopped VPN, so in-flight transport state is not changed.

**Subscription deletion is local only.** It does not revoke the server token. To revoke, use the admin panel. Updating the subscription URL path on Caddy requires users to update the stored URL; the older path is intentionally not kept as an alias.

## Build and tests

```sh
cd ~/src/mbox-main
python3 -m pytest -q tests
GO111MODULE=off go test ./server ./client
(cd android && ./gradlew :app:assembleDebug)
```

Build a properly signed release APK only with the **existing release signing identity**. Debug and release APKs signed with different certificates cannot upgrade each other in place without uninstalling, and uninstalling can remove app data. Confirm package name and signing fingerprint before distribution. No production deployment is needed for this Android-only change.

## Required acceptance checks on a real device

1. Upgrade without uninstalling; old VPN profile still connects via QUIC/HTTPS.
2. Import two distinct HTTPS subscriptions. Ensure both appear in the list after app relaunch.
3. Add two ingress entries to one subscription, refresh, choose each ingress, connect and inspect transport/Noise identity.
4. Update one subscription while leaving the other unchanged. Disconnect physical network and confirm cached profiles remain usable.
5. Import a direct `mbox://` link, switch to a subscription, then select the saved direct profile and connect.
6. Reject an invalid JSON response, 30x redirect, replaced Noise server key, duplicate ingress ID and oversized response without changing the active profile.
7. Test app process death, screen idle and physical network transitions; ensure VPN fail-closed behavior has not regressed.

## QR troubleshooting

Admin-generated PNG QR codes require the **`qrencode` package on VPS**. If the button fails, run `tools/mbox-admin/qr-preflight.sh` on VPS. Install with your distro package manager, then retry. A direct `mbox://` containing long certificate data may exceed QR capacity; prefer a short HTTPS subscription URL. Never paste subscription tokens or private keys into public bug reports.

## VPS source drift audit

Run `tools/mbox-audit-vps.sh ~/src/mbox-main root@YOUR_VPS_IP` from Arch. The script only downloads installed admin/subscription *source files*, and saves a private Caddy/systemd snapshot outside the checkout for review. It reports MATCH/DIFF/ONLY_VPS but **never overwrites Arch**. Review diff per file before importing any difference. Never commit `/etc/mbox`, `users.db`, Caddy private material, signing keys, production certificate keys or private auth data to Git.
