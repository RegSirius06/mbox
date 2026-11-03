from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def text(path: str) -> str:
    return (ROOT / path).read_text(encoding="utf-8")


def test_cronet_dependency_and_transport_policy_are_pinned():
    gradle = text("android/app/build.gradle")
    bridge = text("android/app/src/main/java/io/mbox/android/CronetBridge.java")
    assert "org.chromium.net:cronet-bundled:500.1.0" in gradle
    assert ".enableHttp2(true)" in bridge
    assert ".enableQuic(false)" in bridge
    assert 'InetAddress.getByName("127.0.0.1")' in bridge


def test_cronet_bridge_owns_transport_level_headers():
    bridge = text("android/app/src/main/java/io/mbox/android/CronetBridge.java").lower()
    for header in ("user-agent", "accept-encoding", "host", "content-length", "connection", "transfer-encoding", "upgrade"):
        assert f'n.equals("{header}")' in bridge


def test_android_defaults_to_cronet_and_go_is_diagnostic_only():
    profile = text("android/app/src/main/java/io/mbox/android/ProfileStore.java")
    service = text("android/app/src/main/java/io/mbox/android/MBoxVpnService.java")
    assert 'DEFAULT_ANDROID_HTTP_STACK="cronet"' in profile
    assert '!"cronet".equals(v)&&!"go".equals(v)' in profile
    assert 'Android VPN requires Cronet fail-closed transport' in service
    assert 'DNS_DELEGATED stack=cronet' in service


def test_android_handover_monitor_tracks_only_non_vpn_networks():
    service = text("android/app/src/main/java/io/mbox/android/MBoxVpnService.java")
    monitor = text("android/app/src/main/java/io/mbox/android/UnderlyingNetworkMonitor.java")
    assert "registerDefaultNetworkCallback" not in service
    assert "new UnderlyingNetworkMonitor" in service
    assert ".addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)" in monitor
    assert ".addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)" in monitor
    assert "!capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)" in monitor
    assert "registerNetworkCallback(request, callback)" in monitor
    assert "DEBOUNCE_MS = 250L" in monitor
    assert "awaitUsableNetwork()" in monitor
    assert "handler.postDelayed(reconcileTask, DEBOUNCE_MS)" in monitor
    assert "underlyingChangeGeneration.incrementAndGet()" in service
    assert 'NET stage=NETWORK code=WAIT_UNDERLYING' in service
    assert 'NET stage=NETWORK code=UNDERLYING_AVAILABLE' in service
    assert 'RECONNECT reason=underlying_network immediate=true' in service


def test_android_passes_stable_device_id_to_core():
    service = text("android/app/src/main/java/io/mbox/android/MBoxVpnService.java")
    profile = text("android/app/src/main/java/io/mbox/android/ProfileStore.java")
    assert 'MBOX_DEVICE_ID' in service
    assert 'installationDeviceId' in service
    assert 'PREF_DEVICE_ID' in profile
    assert 'new byte[16]' in profile


def test_hev_udp_relay_is_fail_closed_over_tcp():
    app_runtime = (ROOT / "android/app/src/main/java/io/mbox/android/AppRuntime.java").read_text()
    assert "udp: 'tcp'" in app_runtime
    assert "udp: 'udp'" not in app_runtime


def test_android_vpn_is_fail_closed_during_connect_and_recovery():
    service = text("android/app/src/main/java/io/mbox/android/MBoxVpnService.java")
    manifest = text("android/app/src/main/AndroidManifest.xml")
    assert "addDisallowedApplication" not in service
    assert 'android:value="true"' in manifest
    assert "SUPPORTS_ALWAYS_ON" in manifest
    assert "ensureFailClosedVpnInterface();" in service
    assert service.index("ensureFailClosedVpnInterface();") < service.index("startCoreTransport();")
    assert "cleanupCoreTransport();" in service
    assert "cleanupAllTransport();" in service
    assert "TProxyService.TProxyStopService()" in service
    # Recovery cleanup must not stop HEV or close the TUN; those operations live
    # only in the explicit full-stop cleanup path.
    core_cleanup = service.split("private synchronized void cleanupCoreTransport()", 1)[1].split("private synchronized void cleanupAllTransport()", 1)[0]
    assert "TProxyStopService" not in core_cleanup
    assert "tun.close()" not in core_cleanup
    assert 'State.DISCONNECTED' in service
    assert 'State.RECOVERING' in service
    assert 'State.CONNECTED' in service
    assert 'State.FAILED' in service


def test_android_outer_transport_is_per_engine_bound_not_whole_uid_bypass():
    service = text("android/app/src/main/java/io/mbox/android/MBoxVpnService.java")
    bridge = text("android/app/src/main/java/io/mbox/android/CronetBridge.java")
    assert 'throw new SecurityException("Android VPN requires Cronet fail-closed transport' in service
    assert "engine.bindToNetwork(network.getNetworkHandle())" in bridge
    assert 'throw new IOException("Cronet requires an explicit underlying Network")' in bridge
    assert "if (underlyingNetwork == null)" in service
    assert 'CronetBridge.start(this, endpointUrl, underlyingNetwork, networkDebug)' in service
