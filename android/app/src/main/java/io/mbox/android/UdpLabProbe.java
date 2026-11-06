package io.mbox.android;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;

import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.security.SecureRandom;
import java.util.Arrays;

/** A debug-only STUN probe, forced onto Android's VPN Network, never physical. */
final class UdpLabProbe {
    private static final int STUN_PORT = 19302;
    private static final int MAGIC = 0x2112A442;
    private static final SecureRandom RNG = new SecureRandom();

    static String run(Context context) {
        if (!AppRuntime.udpLabEnabled(context) ||
                MBoxVpnService.getState() != MBoxVpnService.State.CONNECTED) {
            return "UDP LAB: VPN not connected or debug mode unavailable";
        }
        try {
            ConnectivityManager manager = (ConnectivityManager)
                    context.getSystemService(Context.CONNECTIVITY_SERVICE);
            if (manager == null) {
                return "UDP LAB: ConnectivityManager unavailable";
            }
            // Never use the default Network, which can be a physical bypass.
            // Network.bindSocket() pins this socket to the VPN netId. If the
            // VPN disappears, the probe fails rather than falling back.
            Network vpn = null;
            for (Network network : manager.getAllNetworks()) {
                NetworkCapabilities caps = manager.getNetworkCapabilities(network);
                if (caps != null && caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) {
                    vpn = network;
                    break;
                }
            }
            if (vpn == null) {
                return "UDP LAB: no VPN Network (refusing physical network)";
            }
            InetAddress target = null;
            // Resolve through the VPN network as well, not the system DNS.
            for (InetAddress address : vpn.getAllByName("stun.l.google.com")) {
                if (address instanceof Inet4Address) {
                    target = address;
                    break;
                }
            }
            if (target == null) {
                return "UDP LAB: no IPv4 STUN address via VPN DNS";
            }
            try (DatagramSocket socket = new DatagramSocket()) {
                vpn.bindSocket(socket);
                socket.setSoTimeout(6000);
                socket.connect(new InetSocketAddress(target, STUN_PORT));
                byte[] request = new byte[20];
                request[0] = 0;
                request[1] = 1; // STUN Binding Request
                request[4] = 0x21;
                request[5] = 0x12;
                request[6] = (byte) 0xa4;
                request[7] = 0x42;
                byte[] transaction = new byte[12];
                RNG.nextBytes(transaction);
                System.arraycopy(transaction, 0, request, 8, 12);
                long started = System.nanoTime();
                socket.send(new DatagramPacket(request, request.length));
                byte[] raw = new byte[1024];
                DatagramPacket reply = new DatagramPacket(raw, raw.length);
                socket.receive(reply);
                String mapped = mappedIPv4(raw, reply.getLength(), transaction);
                long rttMs = (System.nanoTime() - started) / 1_000_000L;
                return "UDP PASS via VPN: STUN mapped IP " + mapped +
                        ", RTT " + rttMs + " ms. Confirm this IP matches the expected exit.";
            }
        } catch (Exception error) {
            // Do not retry through an unbound or physical socket.
            return "UDP LAB failed (no fallback): " + error.getClass().getSimpleName();
        }
    }

    // RFC 5389: validate the response type, transaction, length and its
    // (XOR-)MAPPED-ADDRESS before reporting success. No external STUN library.
    static String mappedIPv4(byte[] b, int length, byte[] transaction) throws Exception {
        if (length < 20 || (b[0] & 0xff) != 1 || (b[1] & 0xff) != 1 ||
                read32(b, 4) != MAGIC ||
                !Arrays.equals(Arrays.copyOfRange(b, 8, 20), transaction)) {
            throw new IllegalArgumentException("invalid STUN reply");
        }
        int size = read16(b, 2);
        if ((size & 3) != 0 || size > length - 20) {
            throw new IllegalArgumentException("truncated STUN reply");
        }
        int end = 20 + size;
        for (int pos = 20; pos + 4 <= end; ) {
            int type = read16(b, pos);
            int attrLen = read16(b, pos + 2);
            if (attrLen > end - (pos + 4)) {
                break;
            }
            if ((type == 0x0020 || type == 0x0001) && attrLen == 8 &&
                    b[pos + 5] == 1) {
                int ip = read32(b, pos + 8);
                if (type == 0x0020) ip ^= MAGIC;
                return ((ip >>> 24) & 255) + "." + ((ip >>> 16) & 255) + "." +
                        ((ip >>> 8) & 255) + "." + (ip & 255);
            }
            pos += 4 + ((attrLen + 3) & ~3);
        }
        throw new IllegalArgumentException("missing IPv4 mapped address");
    }

    private static int read16(byte[] b, int n) {
        return (b[n] & 255) << 8 | (b[n + 1] & 255);
    }

    private static int read32(byte[] b, int n) {
        return read16(b, n) << 16 | read16(b, n + 2);
    }

    private UdpLabProbe() {}
}
