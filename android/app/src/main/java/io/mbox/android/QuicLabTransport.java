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

/** Debug-only QUIC path for comparison with the existing HTTPS transport. */
final class QuicLabTransport implements Closeable {
    private static final String PREFS = "mbox-transport-lab";
    private static final String ENABLED = "quic-enabled";
    private static final String REMOTE_ASSET = "mbox-quic-remote.txt";
    private static final String CA_ASSET = "mbox-quic-ca.pem";

    private final File workDir;
    private final File socketFile;
    private final QuicSocketProtector protector;
    private final Process process;

    static boolean configured(Context c) {
        if (!AppRuntime.udpLabEnabled(c)) return false;
        try (InputStream u = c.getAssets().open(REMOTE_ASSET);
             InputStream cert = c.getAssets().open(CA_ASSET)) {
            return u.read() >= 0 && cert.read() >= 0;
        } catch (Exception missing) { return false; }
    }

    static boolean enabled(Context c) {
        return configured(c) && c.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean(ENABLED, false);
    }

    static boolean toggle(Context c) {
        boolean next = !enabled(c);
        c.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean(ENABLED, next).apply();
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

    QuicLabTransport(Context context, Network network) throws Exception {
        if (!enabled(context)) throw new IllegalStateException("QUIC debug mode disabled");
        workDir = Files.createTempDirectory(context.getCacheDir().toPath(), "mbox-quic-").toFile();
        socketFile = new File(workDir, "http.sock");
        QuicSocketProtector broker = null;
        Process child = null;
        try {
            File ca = new File(workDir, "ca.pem");
            try (InputStream in = context.getAssets().open(CA_ASSET);
                 FileOutputStream out = new FileOutputStream(ca)) {
                    byte[] buffer = new byte[4096];
                    int n;
                    while ((n = in.read(buffer)) != -1) out.write(buffer, 0, n);
                }
            String remote;
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
            URI uri = URI.create(remote);
            if (!"https".equalsIgnoreCase(uri.getScheme()) || uri.getHost() == null || uri.getPort() < 1 ||
                    uri.getRawUserInfo() != null || uri.getRawQuery() != null || uri.getRawFragment() != null ||
                    !uri.getPath().isEmpty() && !"/".equals(uri.getPath())) {
                throw new SecurityException("Invalid QUIC remote asset");
            }
            // No Java DNS lookup outside the VPN. The stage-1 lab only
            // permits numeric IPv4; the Go gateway enforces this too.
            String[] parts = uri.getHost().split("\\.");
            if (parts.length != 4) throw new SecurityException("QUIC lab requires numeric IPv4");
            for (String part : parts) {
                if (!part.matches("[0-9]{1,3}") || Integer.parseInt(part) > 255)
                    throw new SecurityException("Invalid QUIC lab IPv4 address");
            }
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
            Log.i("MBox", "QUIC Android laboratory gateway ready");
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
