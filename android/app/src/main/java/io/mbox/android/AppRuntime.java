package io.mbox.android;

/**
 * Local plumbing shared by the Android wrapper, the generated MBox client
 * config and HEV. These values never identify a remote server or a user.
 */
final class AppRuntime {
    static final String SOCKS_HOST = "127.0.0.1";
    static final int SOCKS_PORT = 10890;
    static final int MTU = 1500;

    // RFC 2544 benchmarking space is intentionally used for the private TUN.
    static final String TUN_IPV4 = "198.18.0.1";
    static final String TUN_IPV6 = "fdfe:dcba:9876::1";
    static final String MAP_DNS = "198.18.0.2";
    static final String MAP_NETWORK = "100.64.0.0";
    static final String MAP_NETMASK = "255.192.0.0";

    // Diagnostic STUN controls remain restricted to debug builds.
    // Release UDP forwarding uses authenticated MBox frames and is separate.
    static boolean udpLabEnabled(android.content.Context context) {
        return (context.getApplicationInfo().flags &
                android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE) != 0;
    }

    static String socksListen() {
        return SOCKS_HOST + ":" + SOCKS_PORT;
    }

    static String hevConfig() {
        return "tunnel:\n" +
                "  mtu: " + MTU + "\n" +
                "  ipv4: " + TUN_IPV4 + "\n" +
                "  ipv6: '" + TUN_IPV6 + "'\n" +
                "  icmp: 'off'\n" +
                "\n" +
                "socks5:\n" +
                "  address: " + SOCKS_HOST + "\n" +
                "  port: " + SOCKS_PORT + "\n" +
                // Fail closed: HEV must never open a physical UDP relay socket.
                // TCP relay mode keeps UDP in the local HEV/SOCKS connection;
                // production MBox UDP forwarding is authenticated and encrypted.
                // Unsupported destinations fail closed, without direct fallback.
                "  udp: 'tcp'\n" +
                "\n" +
                "mapdns:\n" +
                "  address: " + MAP_DNS + "\n" +
                "  port: 53\n" +
                "  network: " + MAP_NETWORK + "\n" +
                "  netmask: " + MAP_NETMASK + "\n" +
                "  cache-size: 10000\n" +
                "\n" +
                "misc:\n" +
                "  log-level: warn\n";
    }

    private AppRuntime() {}
}
