package io.mbox.android;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.os.SystemClock;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

import javax.net.ssl.HttpsURLConnection;

/** Optional, bounded GitHub release discovery. Never uses the physical network. */
final class ReleaseUpdates {
    private static final String API = "https://api.github.com/repos/RegSirius06/mbox/releases?per_page=5";
    private static final String RELEASES = "https://github.com/RegSirius06/mbox/releases";
    private static final String CHANNEL = "mbox-releases";
    static final String ACTION_STAY = "io.mbox.android.RELEASE_STAY";
    static final String ACTION_GITHUB = "io.mbox.android.RELEASE_GITHUB";
    private static final int NOTIFICATION_ID = 1043;
    private static final String PREFS = "mbox-release-updates";
    private static final long INTERVAL_MS = 6 * 60 * 60 * 1000L;
    private static final int MAX_BYTES = 256 * 1024;
    private static final AtomicBoolean inFlight = new AtomicBoolean();
    private static long lastAttemptElapsed;
    private static final Pattern VERSION = Pattern.compile(
            "^v?(\\d+)\\.(\\d+)\\.(\\d+)(?:-(alpha|beta|rc)\\.(\\d+)(?:\\.(\\d+))?)?$",
            Pattern.CASE_INSENSITIVE);

    static String releasesUrl() { return RELEASES; }

    static void dismiss(Context context) {
        NotificationManager manager = (NotificationManager)
                context.getSystemService(Context.NOTIFICATION_SERVICE);
        if (manager != null) manager.cancel(NOTIFICATION_ID);
    }

    /** Called from visible UI. No worker or timer is started when VPN is down. */
    static void maybeCheck(Context context) {
        if (MBoxVpnService.getState() != MBoxVpnService.State.CONNECTED) return;
        long now = SystemClock.elapsedRealtime();
        synchronized (ReleaseUpdates.class) {
            if (lastAttemptElapsed != 0 && now - lastAttemptElapsed < INTERVAL_MS) return;
            if (!inFlight.compareAndSet(false, true)) return;
            lastAttemptElapsed = now;
        }
        Context app = context.getApplicationContext();
        new Thread(() -> {
            try { check(app); }
            catch (Exception ignored) { /* No update check should interrupt VPN. */ }
            finally { inFlight.set(false); }
        }, "MBox-release-check").start();
    }

    private static void check(Context c) throws Exception {
        // Invariant: bind the HTTPS request to a VPN Network. Never fall back
        // to the app's underlying physical network if VPN is unavailable.
        if (MBoxVpnService.getState() != MBoxVpnService.State.CONNECTED) return;
        ConnectivityManager manager = (ConnectivityManager)
                c.getSystemService(Context.CONNECTIVITY_SERVICE);
        if (manager == null) return;
        Network vpn = manager.getActiveNetwork();
        if (vpn == null) return;
        NetworkCapabilities caps = manager.getNetworkCapabilities(vpn);
        if (caps == null || !caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) return;

        HttpsURLConnection request = (HttpsURLConnection)
                vpn.openConnection(new URL(API));
        try {
            request.setInstanceFollowRedirects(false);
            request.setConnectTimeout(5000);
            request.setReadTimeout(5000);
            request.setRequestProperty("Accept", "application/vnd.github+json");
            request.setRequestProperty("User-Agent", "MBox-Android-release-check");
            if (request.getResponseCode() != 200) return;
            ByteArrayOutputStream body = new ByteArrayOutputStream();
            try (InputStream in = request.getInputStream()) {
                byte[] buffer = new byte[4096];
                int count;
                while ((count = in.read(buffer)) != -1) {
                    if (body.size() + count > MAX_BYTES) return;
                    body.write(buffer, 0, count);
                }
            }
            JSONArray releases = new JSONArray(new String(body.toByteArray(), StandardCharsets.UTF_8));
            String current = c.getPackageManager().getPackageInfo(c.getPackageName(), 0).versionName;
            String best = null;
            for (int i = 0; i < releases.length(); i++) {
                JSONObject release = releases.optJSONObject(i);
                if (release == null || release.optBoolean("draft", true)) continue;
                String candidate = release.optString("tag_name", "");
                if (newer(candidate, current) && (best == null || newer(candidate, best)))
                    best = candidate;
            }
            if (best == null || MBoxVpnService.getState() != MBoxVpnService.State.CONNECTED)
                return;
            SharedPreferences prefs = c.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
            if (best.equals(prefs.getString("last_notified", ""))) return;
            if (notifyUpdate(c, best)) prefs.edit().putString("last_notified", best).apply();
        } finally {
            request.disconnect();
        }
    }

    /** Semver with MBox's beta.N.M prerelease scheme; unsupported tags are ignored. */
    static boolean newer(String candidate, String current) {
        int[] a = parts(candidate), b = parts(current);
        if (a == null || b == null) return false;
        for (int i = 0; i < a.length; i++) {
            if (a[i] != b[i]) return a[i] > b[i];
        }
        return false;
    }

    private static int[] parts(String version) {
        if (version == null) return null;
        Matcher m = VERSION.matcher(version);
        if (!m.matches()) return null;
        try {
            String phase = m.group(4);
            int rank = phase == null ? 4 : "rc".equalsIgnoreCase(phase) ? 3
                    : "beta".equalsIgnoreCase(phase) ? 2 : 1;
            return new int[] { Integer.parseInt(m.group(1)), Integer.parseInt(m.group(2)),
                    Integer.parseInt(m.group(3)), rank,
                    m.group(5) == null ? 0 : Integer.parseInt(m.group(5)),
                    m.group(6) == null ? 0 : Integer.parseInt(m.group(6)) };
        } catch (NumberFormatException invalid) { return null; }
    }

    private static boolean notifyUpdate(Context c, String version) {
        NotificationManager manager = (NotificationManager)
                c.getSystemService(Context.NOTIFICATION_SERVICE);
        if (manager == null || !manager.areNotificationsEnabled()) return false;
        manager.createNotificationChannel(new NotificationChannel(
                CHANNEL, "MBox releases", NotificationManager.IMPORTANCE_DEFAULT));
        NotificationChannel channel = manager.getNotificationChannel(CHANNEL);
        if (channel == null || channel.getImportance() == NotificationManager.IMPORTANCE_NONE)
            return false;
        Intent app = new Intent(c, MainActivity.class)
                .setAction(ACTION_STAY)
                .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        PendingIntent stay = PendingIntent.getActivity(c, 103, app,
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        Intent openGithub = new Intent(c, MainActivity.class)
                .setAction(ACTION_GITHUB)
                .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP | Intent.FLAG_ACTIVITY_CLEAR_TOP);
        PendingIntent github = PendingIntent.getActivity(c, 104, openGithub,
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        boolean ru = UiLanguage.russian(c);
        String title = ru ? "Доступно обновление MBox" : "MBox update available";
        String message = ru ? "Версия " + version + " · подробности на GitHub"
                : "Version " + version + " · see GitHub releases";
        Notification notification = new Notification.Builder(c, CHANNEL)
                .setSmallIcon(android.R.drawable.stat_sys_download_done)
                .setContentTitle(title)
                .setContentText(message)
                .setContentIntent(stay)
                .setAutoCancel(true)
                .setOnlyAlertOnce(true)
                .addAction(android.R.drawable.ic_menu_close_clear_cancel,
                        ru ? "Остаться в MBox" : "Stay in MBox", stay)
                .addAction(android.R.drawable.ic_menu_view,
                        ru ? "Открыть GitHub" : "Open GitHub", github)
                .build();
        manager.notify(NOTIFICATION_ID, notification);
        return true;
    }

    private ReleaseUpdates() {}
}
