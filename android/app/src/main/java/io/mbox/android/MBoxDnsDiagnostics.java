package io.mbox.android;

import android.util.Log;
import android.net.Network;
import java.net.InetAddress;
import java.util.LinkedHashSet;
import java.util.concurrent.TimeUnit;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/** Diagnostic-only DNS reporting; never logs a full IP unless explicitly enabled. */
final class MBoxDnsDiagnostics {
    private static final String TAG = "MBox";

    static void logDnsResult(String host, List<String> addresses, boolean debug) {
        int v4 = 0;
        int v6 = 0;
        List<String> shown = new ArrayList<>();
        for (String ip : addresses) {
            if (ip.contains(":")) {
                v6++;
            } else {
                v4++;
            }
            shown.add(debug ? ip : redactAddress(ip));
        }
        Log.i(TAG, "NET stage=DNS code=DNS_OK host=" + host
                + " count=" + addresses.size() + " ipv4=" + v4 + " ipv6=" + v6
                + " addresses=" + String.join(",", shown));
    }

    private static String redactAddress(String value) {
        try {
            byte[] digest = MessageDigest.getInstance("SHA-256")
                    .digest(value.getBytes(StandardCharsets.UTF_8));
            return String.format(Locale.ROOT, "ip#%02x%02x%02x%02x",
                    digest[0] & 0xff, digest[1] & 0xff, digest[2] & 0xff, digest[3] & 0xff);
        } catch (Exception ignored) {
            return "ip#redacted";
        }
    }

    /** Diagnostic mode only; all lookups use the selected physical Network. */
    static List<String> resolveEndpointAddresses(Network network, String host) throws Exception {
        if (network == null) {
            Log.e(TAG, "NET stage=DNS code=ERR_DNS_NO_NETWORK");
            throw new Exception("ERR_DNS: нет активной физической сети");
        }
        final InetAddress[] answers;
        long started = System.nanoTime();
        try {
            answers = network.getAllByName(host);
        } catch (Exception error) {
            String message = error.getMessage() == null ? error.getClass().getSimpleName() : error.getMessage();
            message = message.substring(0, Math.min(180, message.length()));
            Log.e(TAG, "NET stage=DNS code=ERR_DNS host=" + host + " error=" + MBoxLogSanitizer.sanitize(message));
            throw new Exception("ERR_DNS: " + message, error);
        }
        long elapsedMs = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - started);
        LinkedHashSet<String> unique = new LinkedHashSet<>();
        for (InetAddress answer : answers) {
            String ip = answer.getHostAddress();
            int zone = ip.indexOf('%');
            if (zone >= 0) ip = ip.substring(0, zone);
            if (!ip.isEmpty()) unique.add(ip);
            if (unique.size() >= 16) break;
        }
        if (unique.isEmpty()) {
            Log.e(TAG, "NET stage=DNS code=ERR_DNS_EMPTY host=" + host + " time=" + elapsedMs + "ms");
            throw new Exception("ERR_DNS: DNS не вернул адрес для " + host);
        }
        return new ArrayList<>(unique);
    }

    private MBoxDnsDiagnostics() { }
}
