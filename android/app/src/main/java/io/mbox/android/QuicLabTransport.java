package io.mbox.android;

import android.content.Context;
import android.net.Network;
import android.util.Log;

import org.json.JSONObject;

import java.io.Closeable;
import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.concurrent.TimeUnit;

/** Protected native QUIC gateway; debug assets are permitted only in debug APKs. */
final class QuicLabTransport implements Closeable {
    private static final String PREFS = "mbox-transport-lab";
    private static final String ENABLED = "quic-enabled"; // legacy two-state preference
    private static final String MODE = "mode-v1";
    private static final String REMOTE_ASSET = "mbox-quic-remote.txt";
    private static final String CA_ASSET = "mbox-quic-ca.pem";

    private final File workDir;
    private final File socketFile;
    private final QuicSocketProtector protector;
    private final Process process;

    static boolean configured(Context c) {
        try { if (ProfileStore.hasProfile(c) && ProfileStore.quicConfig(c)!=null) return true; }
        catch (Exception invalid) { return false; }
        if (!AppRuntime.udpLabEnabled(c)) return false;
        try (InputStream u = c.getAssets().open(REMOTE_ASSET);
             InputStream cert = c.getAssets().open(CA_ASSET)) {
            return u.read() >= 0 && cert.read() >= 0;
        } catch (Exception missing) { return false; }
    }

    static TransportPolicy.Mode mode(Context c) {
        if (!configured(c)) return TransportPolicy.Mode.HTTPS;
        android.content.SharedPreferences prefs = c.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        String selected = prefs.getString(MODE, null);
        if (selected != null) {
            try { return TransportPolicy.Mode.valueOf(selected); }
            catch (IllegalArgumentException ignored) { return TransportPolicy.Mode.HTTPS; }
        }
        // Preserve a user's explicit old laboratory selection on upgrade.
        if (prefs.contains(ENABLED)) return prefs.getBoolean(ENABLED, false)
                ? TransportPolicy.Mode.QUIC : TransportPolicy.Mode.HTTPS;
        return TransportPolicy.Mode.AUTO;
    }

    static TransportPolicy.Mode cycleMode(Context c) {
        TransportPolicy.Mode current = mode(c);
        TransportPolicy.Mode next = current == TransportPolicy.Mode.AUTO ? TransportPolicy.Mode.HTTPS
                : current == TransportPolicy.Mode.HTTPS ? TransportPolicy.Mode.QUIC
                : TransportPolicy.Mode.AUTO;
        if (!c.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit()
                .putString(MODE, next.name()).commit()) {
            throw new IllegalStateException("Cannot persist transport selection");
        }
        return next;
    }

    static void rewriteRuntimeConfig(File configFile) throws Exception {
        // Only a temporary decrypted runtime copy is changed, never the profile.
        JSONObject temporary = new JSONObject(new String(
                Files.readAllBytes(configFile.toPath()), StandardCharsets.UTF_8));
        URI original = new URI(temporary.getJSONObject("transport").getString("url"));
        temporary.getJSONObject("transport").put("url", "http://127.0.0.1:18451" + original.getRawPath());
        Files.write(configFile.toPath(), temporary.toString().getBytes(StandardCharsets.UTF_8));
    }

    String unixPath() { return socketFile.getAbsolutePath(); }
    boolean isAlive() { return process.isAlive(); }

    QuicLabTransport(Context context, Network network) throws Exception {
        if (!configured(context)) throw new IllegalStateException("QUIC not configured in profile");
        workDir = Files.createTempDirectory(context.getCacheDir().toPath(), "mbox-quic-").toFile();
        socketFile = new File(workDir, "http.sock");
        QuicSocketProtector broker = null;
        Process child = null;
        try {
            File ca = new File(workDir, "ca.pem");
            ProfileStore.QuicConfig profileQuic = ProfileStore.quicConfig(context);
            String remote;
            if (profileQuic != null) {
                // Public certificate and destination come from the imported profile,
                // not from an APK-wide developer endpoint.
                remote = profileQuic.url;
                Files.write(ca.toPath(), profileQuic.caPem.getBytes(StandardCharsets.US_ASCII));
            } else {
                if (!AppRuntime.udpLabEnabled(context)) throw new SecurityException("Profile has no QUIC configuration");
                try (InputStream in = context.getAssets().open(CA_ASSET);
                     FileOutputStream out = new FileOutputStream(ca)) {
                    byte[] buffer = new byte[4096];
                    int n;
                    while ((n = in.read(buffer)) != -1) out.write(buffer, 0, n);
                }
                try (InputStream in = context.getAssets().open(REMOTE_ASSET)) {
                    java.io.ByteArrayOutputStream bytes = new java.io.ByteArrayOutputStream();
                    byte[] buffer = new byte[1024];
                    int n;
                    while ((n = in.read(buffer)) != -1) {
                        if (bytes.size() + n > 512) throw new SecurityException("QUIC endpoint too long");
                        bytes.write(buffer, 0, n);
                    }
                    remote = bytes.toString(StandardCharsets.UTF_8.name()).trim();
                }
            }
            ProfileStore.validateQuicURL(remote);
            // Public routing metadata only; never give the QUIC gateway MBox keys.
            JSONObject original = new JSONObject(new String(Files.readAllBytes(ProfileStore.configFile(context).toPath()), StandardCharsets.UTF_8));
            JSONObject routes = new JSONObject();
            JSONObject t = new JSONObject();
            t.put("base_path", new URI(original.getJSONObject("transport").getString("url")).getRawPath());
            routes.put("transport", t);
            JSONObject oldCamouflage = original.getJSONObject("camouflage");
            JSONObject camo = new JSONObject();
            for (String field : new String[]{"open_path", "up_path", "down_path", "close_path"}) {
                if (oldCamouflage.has(field)) camo.put(field, oldCamouflage.getString(field));
            }
            routes.put("camouflage", camo);
            File routesFile = new File(workDir, "routes.json");
            Files.write(routesFile.toPath(), routes.toString().getBytes(StandardCharsets.UTF_8));

            broker = new QuicSocketProtector(network);
            String binary = context.getApplicationInfo().nativeLibraryDir + "/libmboxquic.so";
            child = new ProcessBuilder(binary, "client", "--unix-listen", socketFile.getAbsolutePath(),
                    "--remote", remote, "--ca", ca.getAbsolutePath(),
                    "--server-config", routesFile.getAbsolutePath(),
                    "--protect-socket", broker.socketAddress(),
                    "--protect-token", broker.token())
                    .redirectErrorStream(true).start();
            final Process loggedProcess = child;
            new Thread(() -> {
                try (java.io.BufferedReader lines = new java.io.BufferedReader(
                        new java.io.InputStreamReader(loggedProcess.getInputStream()))) {
                    String line;
                    while ((line = lines.readLine()) != null) Log.i("MBox", "QUIC: " + MBoxLogSanitizer.sanitize(line));
                } catch (Exception ignored) { }
            }, "MBox-quic-log").start();
            for (int i = 0; i < 50 && !socketFile.exists() && child.isAlive(); i++) {
                Thread.sleep(100);
            }
            if (!socketFile.exists() || !child.isAlive()) throw new Exception("QUIC gateway failed to start");
            protector = broker;
            process = child;
            Log.i("MBox", "QUIC Android gateway ready");
        } catch (Exception failure) {
            if (child != null) child.destroyForcibly();
            if (broker != null) broker.close();
            removeWorkDir(workDir);
            throw failure;
        }
    }

    @Override public void close() {
        process.destroy();
        try { if (!process.waitFor(1, TimeUnit.SECONDS)) process.destroyForcibly(); }
        catch (InterruptedException stop) { Thread.currentThread().interrupt(); process.destroyForcibly(); }
        protector.close();
        removeWorkDir(workDir);
    }

    private static void removeWorkDir(File root) {
        File[] files = root.listFiles();
        if (files != null) for (File file : files) file.delete();
        root.delete();
    }
}
