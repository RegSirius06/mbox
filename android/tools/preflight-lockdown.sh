#!/usr/bin/env bash
set -euo pipefail
command -v adb >/dev/null || { echo 'adb not found' >&2; exit 2; }
APP='io.mbox.android'
DEVICE="$(adb get-state 2>/dev/null)"
[[ "$DEVICE" == device ]] || { echo 'adb device not ready' >&2; exit 2; }
ALWAYS="$(adb shell settings get secure always_on_vpn_app | tr -d '\r')"
LOCKDOWN="$(adb shell settings get secure always_on_vpn_lockdown | tr -d '\r')"
[[ "$ALWAYS" == "$APP" ]] || {
  echo "FAIL: Always-on VPN is not set to ${APP} (value omitted)" >&2
  echo 'Android Settings -> Network & internet -> VPN -> MBox -> Always-on VPN' >&2
  exit 1
}
[[ "$LOCKDOWN" == '1' ]] || {
  echo 'FAIL: Android VPN lockdown is not enabled' >&2
  echo 'Enable "Block connections without VPN" before crash/handover tests.' >&2
  exit 1
}
echo 'PASS: Android settings report MBox Always-on and lockdown enabled.'
echo 'NOTE: ADB settings are not packet-capture proof. Capture on Wi-Fi/router and mobile network still required.'
