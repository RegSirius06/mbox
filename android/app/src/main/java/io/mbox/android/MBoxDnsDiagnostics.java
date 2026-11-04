package io.mbox.android;

import android.util.Log;
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

    private MBoxDnsDiagnostics() { }
}
