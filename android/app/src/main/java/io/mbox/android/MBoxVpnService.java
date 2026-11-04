package io.mbox.android;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.VpnService;
import android.os.ParcelFileDescriptor;
import android.os.SystemClock;
import android.util.Log;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStreamReader;
import java.net.InetAddress;
import java.security.MessageDigest;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.ThreadLocalRandom;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

import hev.htproxy.TProxyService;

/** Owns the Android TUN, HEV tun2socks and the native MBox client process. */
public class MBoxVpnService extends VpnService {
    public static final String ACTION_START = "io.mbox.android.START";
    public static final String ACTION_STOP = "io.mbox.android.STOP";

    public enum State { DISCONNECTED, CONNECTING, CONNECTED, RECOVERING, FAILED, STOPPED }

    private static final String TAG = "MBox";
    private static final String CHANNEL_ID = "mbox-vpn";
    private static final int NOTIFICATION_ID = 1;
    private static final String STATE_PREFS = "mbox-vpn-state";
    private static final String PREF_DESIRED_RUNNING = "desired_running";

    private static final long RECONNECT_MIN_MS = 1_000;
    private static final long RECONNECT_MAX_MS = 30_000;
    private static final long STABLE_CONNECTION_MS = 30_000;

    private static volatile State state = State.STOPPED;
    private static volatile String lastError = "";

    private final AtomicBoolean stoppedLogged = new AtomicBoolean(true);
    private final AtomicLong underlyingChangeGeneration = new AtomicLong();

    private volatile boolean desiredRunning;
    private volatile boolean cleaning;
    private Thread supervisor;
    private Process mbox;
    private ParcelFileDescriptor tun;
    private CronetBridge cronetBridge;
    private ConnectivityManager connectivityManager;
    private UnderlyingNetworkMonitor networkMonitor;

    public static State getState() {
        return state;
    }

    public static String getLastError() {
        return lastError;
    }

    @Override
    public void onCreate() {
        super.onCreate();
        NotificationManager manager = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        manager.createNotificationChannel(new NotificationChannel(
                CHANNEL_ID, "MBox VPN", NotificationManager.IMPORTANCE_LOW));
        desiredRunning = getSharedPreferences(STATE_PREFS, MODE_PRIVATE)
                .getBoolean(PREF_DESIRED_RUNNING, false);
        connectivityManager = (ConnectivityManager) getSystemService(CONNECTIVITY_SERVICE);
        networkMonitor = new UnderlyingNetworkMonitor(connectivityManager, (previous, current) -> {
            if (!desiredRunning) {
                return;
            }
            underlyingChangeGeneration.incrementAndGet();
            Log.i(TAG, "NET stage=NETWORK code="
                    + (current == null ? "UNDERLYING_LOST" : "UNDERLYING_CHANGED"));
            restartCoreForNetworkChange();
        });
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? null : intent.getAction();
        if (ACTION_STOP.equals(action)) {
            setDesiredRunning(false);
            requestStop();
            return START_NOT_STICKY;
        }
        if (ACTION_START.equals(action)) {
            setDesiredRunning(true);
        } else if (!desiredRunning) {
            // Android may recreate an Always-on VPN service with a null intent.
            // Treat the platform Always-on state as an explicit request to keep
            // the fail-closed TUN alive across process/service restarts.
            if (android.os.Build.VERSION.SDK_INT >= 24 && isAlwaysOn()) {
                setDesiredRunning(true);
            } else {
                return START_NOT_STICKY;
            }
        }
        beginSupervisorIfNeeded();
        return START_STICKY;
    }

    private void setDesiredRunning(boolean running) {
        desiredRunning = running;
        if (!getSharedPreferences(STATE_PREFS, MODE_PRIVATE).edit().putBoolean(PREF_DESIRED_RUNNING, running).commit()) {
            Log.e(TAG, "Failed to persist desired VPN state");
        }
    }

    private void beginSupervisorIfNeeded() {
        desiredRunning = true;
        stoppedLogged.set(false);
        ensureForeground("Connecting…");
        synchronized (this) {
            if (supervisor == null || !supervisor.isAlive()) {
                supervisor = new Thread(this::runSupervisor, "MBox-supervisor");
                supervisor.start();
            }
        }
    }

    private void runSupervisor() {
        long reconnectDelay = RECONNECT_MIN_MS;
        boolean hasConnected = false;

        try {
            // Fail-closed: install the full-route TUN before any outer connection.
            ensureFailClosedVpnInterface();

            while (desiredRunning) {
                if (!awaitUnderlyingNetworkIfNeeded()) {
                    break;
                }
                long attemptNetworkGeneration = underlyingChangeGeneration.get();

                updateState(hasConnected ? State.RECOVERING : State.CONNECTING, "");
                updateNotification(hasConnected ? "Recovering…" : "Connecting…");

                try {
                    Process process = startCoreTransport();
                    hasConnected = true;
                    long connectedAt = SystemClock.elapsedRealtime();

                    updateState(State.CONNECTED, "");
                    updateNotification("Connected: " + ProfileStore.profileName(this));
                    Log.i(TAG, "VPN CONNECTED");

                    int code = process.waitFor();
                    long connectedFor = SystemClock.elapsedRealtime() - connectedAt;
                    if (connectedFor >= STABLE_CONNECTION_MS) {
                        reconnectDelay = RECONNECT_MIN_MS;
                    }
                    if (!desiredRunning || process != mbox) {
                        break;
                    }

                    if (code == 0) {
                        Log.i(TAG, "MBox core exited: 0");
                    } else {
                        Log.e(TAG, "MBox core exited unexpectedly: " + code);
                    }

                    // Keep TUN+HEV installed while the outer transport is down.
                    cleanupCoreTransport();
                    if (!desiredRunning) {
                        break;
                    }
                    updateState(State.RECOVERING, "MBox core завершился: " + code);
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                    break;
                } catch (Throwable error) {
                    if (!desiredRunning) {
                        break;
                    }
                    String message = error.getMessage() == null ? error.toString() : error.getMessage();
                    Log.w(TAG, "VPN transport attempt failed: " + MBoxLogSanitizer.sanitize(message));
                    updateState(State.RECOVERING, MBoxLogSanitizer.sanitize(message));
                    cleanupCoreTransport();
                }

                if (!desiredRunning) {
                    break;
                }

                boolean underlyingChanged = underlyingChangeGeneration.get() != attemptNetworkGeneration;
                if (!underlyingChanged && networkMonitor != null && networkMonitor.currentNetwork() == null) {
                    underlyingChanged = true;
                }
                if (underlyingChanged) {
                    reconnectDelay = RECONNECT_MIN_MS;
                    if (!awaitUnderlyingNetworkIfNeeded()) {
                        break;
                    }
                    Log.i(TAG, "RECONNECT reason=underlying_network immediate=true");
                    continue;
                }

                long actualDelay = jitteredReconnectDelay(reconnectDelay);
                updateNotification("Recovering in " + formatDelay(actualDelay) + "…");
                Log.i(TAG, "RECONNECT base=" + reconnectDelay + "ms actual=" + actualDelay + "ms");

                try {
                    Thread.sleep(actualDelay);
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                    break;
                }

                reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
            }
        } catch (Throwable fatal) {
            if (desiredRunning) {
                String message = fatal.getMessage() == null ? fatal.toString() : fatal.getMessage();
                Log.e(TAG, "Fail-closed VPN interface failed: " + MBoxLogSanitizer.sanitize(message));
                updateState(State.FAILED, MBoxLogSanitizer.sanitize(message));
                updateNotification("VPN failed closed: " + MBoxLogSanitizer.sanitize(safeMessage(fatal)));
            }
        } finally {
            cleanupCoreTransport();
            synchronized (this) {
                if (Thread.currentThread() == supervisor) {
                    supervisor = null;
                }
            }

            if (!desiredRunning) {
                cleanupAllTransport();
                updateState(State.STOPPED, "");
                logStoppedOnce();
                stopForeground(STOP_FOREGROUND_REMOVE);
                stopSelf();
            }
        }
    }
    private boolean awaitUnderlyingNetworkIfNeeded() {
        UnderlyingNetworkMonitor monitor = networkMonitor;
        if (monitor == null || monitor.currentNetwork() != null) {
            return desiredRunning;
        }

        updateState(State.DISCONNECTED, "Ожидание физической сети");
        updateNotification("Waiting for network…");
        Log.i(TAG, "NET stage=NETWORK code=WAIT_UNDERLYING");

        Network network;
        try {
            network = monitor.awaitUsableNetwork();
        } catch (InterruptedException interrupted) {
            Thread.currentThread().interrupt();
            return false;
        }
        if (!desiredRunning || network == null) {
            return false;
        }

        Log.i(TAG, "NET stage=NETWORK code=UNDERLYING_AVAILABLE");
        return true;
    }

    private static long jitteredReconnectDelay(long base) {
        long low = Math.max(1, base * 80 / 100);
        long high = Math.max(low + 1, base * 120 / 100 + 1);
        return ThreadLocalRandom.current().nextLong(low, high);
    }

    private synchronized void restartCoreForNetworkChange() {
        Process process = mbox;
        if (process != null && process.isAlive()) {
            process.destroy();
        }
    }

    private static String formatDelay(long millis) {
        if (millis < 1_000) {
            return millis + " ms";
        }
        if (millis % 1_000 == 0) {
            return (millis / 1_000) + " s";
        }
        return String.format(java.util.Locale.ROOT, "%.1f s", millis / 1000.0);
    }

    private void ensureForeground(String text) {
        Intent openApp = new Intent(this, MainActivity.class);
        PendingIntent pending = PendingIntent.getActivity(
                this, 0, openApp, PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);

        Notification notification = new Notification.Builder(this, CHANNEL_ID)
                .setContentTitle("MBox")
                .setContentText(text)
                .setContentIntent(pending)
                .setOngoing(true)
                .setSmallIcon(android.R.drawable.stat_sys_download_done)
                .build();
        if (android.os.Build.VERSION.SDK_INT >= 34) {
            startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED);
        } else {
            startForeground(NOTIFICATION_ID, notification);
        }
    }

    private void updateNotification(String text) {
        Notification notification = new Notification.Builder(this, CHANNEL_ID)
                .setContentTitle("MBox")
                .setContentText(text)
                .setOngoing(true)
                .setSmallIcon(android.R.drawable.stat_sys_download_done)
                .build();
        ((NotificationManager) getSystemService(NOTIFICATION_SERVICE))
                .notify(NOTIFICATION_ID, notification);
    }

    private static void updateState(State newState, String error) {
        state = newState;
        lastError = MBoxLogSanitizer.sanitize(error);
    }

    private synchronized Process startCoreTransport() throws Exception {
        cleanupCoreTransport();

        if (!desiredRunning) {
            throw new InterruptedException("VPN stopped");
        }

        if (!ProfileStore.configFile(this).isFile()) {
            throw new Exception("Профиль MBox не импортирован");
        }

        String mboxBinary = getApplicationInfo().nativeLibraryDir + "/libmbox.so";
        Log.i(TAG, "MBox binary: " + mboxBinary);

        String endpointHost = ProfileStore.transportHost(this);
        String endpointUrl = ProfileStore.transportUrl(this);
        boolean networkDebug = ProfileStore.networkDebug(this);
        String httpStack = ProfileStore.androidHttpStack(this);
        if (!"cronet".equals(httpStack)) {
            throw new SecurityException("Android VPN requires Cronet fail-closed transport; Go HTTP is diagnostic-only");
        }
        List<String> endpointAddresses = new ArrayList<>();
        if (networkDebug) {
            endpointAddresses = resolveEndpointAddresses(endpointHost);
            logDnsResult(endpointHost, endpointAddresses, true);
        } else {
            // Avoid an extra resolver query in the normal Cronet persona. Cronet owns
            // DNS/address racing on the explicitly bound physical Network.
            Log.i(TAG, "NET stage=DNS code=DNS_DELEGATED stack=cronet");
        }

        Network underlyingNetwork = networkMonitor != null
                ? networkMonitor.currentNetwork()
                : connectivityManager.getActiveNetwork();
        if (underlyingNetwork == null) {
            throw new Exception("No physical network available for Cronet transport");
        }
        cronetBridge = CronetBridge.start(this, endpointUrl, underlyingNetwork, networkDebug);

        CountDownLatch coreReady = new CountDownLatch(1);
        // Persisted PSK stays Keystore-encrypted; plaintext exists only until
        // the core loads it. try-with-resources covers every failure path.
        try (ProfileStore.RuntimeMaterialization runtimeProfile = ProfileStore.materializeRuntimeConfig(this)) {
            ProcessBuilder processBuilder = new ProcessBuilder(
                    mboxBinary, "-config", runtimeProfile.configFile.getAbsolutePath())
                    .redirectErrorStream(true);
            if (!endpointAddresses.isEmpty()) {
                processBuilder.environment().put("MBOX_CONNECT_ADDRS", String.join(",", endpointAddresses));
                processBuilder.environment().put("MBOX_CONNECT_IP", endpointAddresses.get(0));
            }
            if (cronetBridge != null) {
                processBuilder.environment().put("MBOX_HTTP_BRIDGE_ADDR", cronetBridge.address());
                processBuilder.environment().put("MBOX_HTTP_BRIDGE_TOKEN", cronetBridge.token());
            }
            processBuilder.environment().put("MBOX_DEVICE_ID", ProfileStore.installationDeviceId(this));
            Process process = processBuilder.start();
            mbox = process;
            new Thread(() -> readCoreLog(process, coreReady), "MBox-log").start();
            waitForCore(process, coreReady);
            if (!desiredRunning) {
                throw new InterruptedException("VPN stopped");
            }
            return process;
        }
    }

    private List<String> resolveEndpointAddresses(String host) throws Exception {
        ConnectivityManager manager = connectivityManager != null
                ? connectivityManager
                : (ConnectivityManager) getSystemService(CONNECTIVITY_SERVICE);
        Network network = networkMonitor != null ? networkMonitor.currentNetwork() : manager.getActiveNetwork();
        if (network == null) {
            Log.e(TAG, "NET stage=DNS code=ERR_DNS_NO_NETWORK");
            throw new Exception("ERR_DNS: нет активной физической сети");
        }

        final InetAddress[] answers;
        long started = System.nanoTime();
        try {
            answers = network.getAllByName(host);
        } catch (Exception error) {
            Log.e(TAG, "NET stage=DNS code=ERR_DNS host=" + host + " error=" + safeMessage(error));
            throw new Exception("ERR_DNS: " + safeMessage(error), error);
        }
        long elapsedMs = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - started);
        LinkedHashSet<String> unique = new LinkedHashSet<>();
        for (InetAddress answer : answers) {
            String ip = answer.getHostAddress();
            int zone = ip.indexOf('%');
            if (zone >= 0) {
                ip = ip.substring(0, zone);
            }
            if (!ip.isEmpty()) {
                unique.add(ip);
            }
            if (unique.size() >= 16) {
                break;
            }
        }
        if (unique.isEmpty()) {
            Log.e(TAG, "NET stage=DNS code=ERR_DNS_EMPTY host=" + host + " time=" + elapsedMs + "ms");
            throw new Exception("ERR_DNS: DNS не вернул адрес для " + host);
        }
        return new ArrayList<>(unique);
    }

    private void logDnsResult(String host, List<String> addresses, boolean debug) {
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

    private static String safeMessage(Throwable error) {
        String value = error.getMessage();
        if (value == null || value.isEmpty()) {
            value = error.getClass().getSimpleName();
        }
        return value.length() > 180 ? value.substring(0, 180) : value;
    }

    private void readCoreLog(Process process, CountDownLatch ready) {
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(process.getInputStream()))) {
            String line;
            while ((line = reader.readLine()) != null) {
                Log.i(TAG, "core: " + MBoxLogSanitizer.sanitize(line));
                if (line.contains("SOCKS listening on")) {
                    ready.countDown();
                }
            }
        } catch (Exception error) {
            if (!cleaning && desiredRunning && process == mbox) {
                Log.w(TAG, "core log reader stopped: " + MBoxLogSanitizer.sanitize(error.toString()));
            }
        }
    }

    private void waitForCore(Process process, CountDownLatch ready) throws Exception {
        long deadline = System.currentTimeMillis() + 15_000;
        while (System.currentTimeMillis() < deadline) {
            if (!desiredRunning) {
                throw new InterruptedException("VPN stopped");
            }
            if (ready.await(200, TimeUnit.MILLISECONDS)) {
                return;
            }
            if (!process.isAlive()) {
                throw new Exception("MBox core завершился до запуска SOCKS");
            }
        }
        throw new Exception("MBox core не поднял SOCKS за 15 секунд");
    }

    private void requestStop() {
        setDesiredRunning(false);

        Thread thread;
        synchronized (this) {
            thread = supervisor;
        }
        if (thread != null) {
            thread.interrupt();
        }

        cleanupAllTransport();
        updateState(State.STOPPED, "");
        logStoppedOnce();
        stopForeground(STOP_FOREGROUND_REMOVE);
        stopSelf();
    }

    private synchronized void ensureFailClosedVpnInterface() throws Exception {
        if (tun != null && TProxyService.TProxyIsRunning()) {
            return;
        }
        if (tun == null) {
            Builder builder = new Builder()
                    .setSession("MBox")
                    .setMtu(AppRuntime.MTU)
                    .addAddress(AppRuntime.TUN_IPV4, 32)
                    .addAddress(AppRuntime.TUN_IPV6, 128)
                    .addRoute("0.0.0.0", 0)
                    .addRoute("::", 0)
                    .addDnsServer(AppRuntime.MAP_DNS);

            // Do NOT exclude the application UID. Only the Cronet engine is
            // explicitly bound to the selected underlying physical Network.
            // An arbitrary socket created elsewhere in this app must stay in
            // the VPN and therefore cannot silently become a bypass path.
            tun = builder.establish();
            if (tun == null) {
                throw new Exception("Android не создал fail-closed VPN TUN");
            }
        }

        File hevConfig = new File(getFilesDir(), "hev.yml");
        try (FileOutputStream out = new FileOutputStream(hevConfig, false)) {
            out.write(AppRuntime.hevConfig().getBytes(StandardCharsets.UTF_8));
            out.getFD().sync();
        }
        if (!TProxyService.TProxyIsRunning() && !TProxyService.TProxyStartService(hevConfig.getAbsolutePath(), tun.getFd())) {
            // Keep the TUN descriptor installed on failure. With the full-route
            // VPN still active packets are dropped/blocked rather than falling
            // back to the physical network.
            throw new Exception("HEV tun2socks не запустился; TUN оставлен fail-closed");
        }
        if (android.os.Build.VERSION.SDK_INT >= 29) {
            Log.i(TAG, "VPN MODE always_on=" + isAlwaysOn() + " lockdown=" + isLockdownEnabled());
        } else if (android.os.Build.VERSION.SDK_INT >= 24) {
            Log.i(TAG, "VPN MODE always_on=" + isAlwaysOn());
        }
        Log.i(TAG, "VPN FAIL_CLOSED_TUN_READY");
    }

    private synchronized void cleanupCoreTransport() {
        cleaning = true;
        try {
            Process process = mbox;
            mbox = null;
            if (process != null && process.isAlive()) {
                process.destroy();
                try {
                    if (!process.waitFor(2, TimeUnit.SECONDS)) {
                        process.destroyForcibly();
                    }
                } catch (InterruptedException error) {
                    Thread.currentThread().interrupt();
                    process.destroyForcibly();
                }
            }

            CronetBridge bridge = cronetBridge;
            cronetBridge = null;
            if (bridge != null) {
                try {
                    bridge.close();
                } catch (Throwable error) {
                    Log.w(TAG, "Cronet bridge stop failed", error);
                }
            }
        } finally {
            cleaning = false;
        }
    }

    private synchronized void cleanupAllTransport() {
        cleanupCoreTransport();
        try {
            if (TProxyService.TProxyIsRunning()) {
                TProxyService.TProxyStopService();
            }
        } catch (Throwable error) {
            Log.w(TAG, "HEV stop failed", error);
        }
        try {
            if (tun != null) {
                tun.close();
            }
        } catch (Exception ignored) {
        }
        tun = null;
    }

    private void logStoppedOnce() {
        if (stoppedLogged.compareAndSet(false, true)) {
            Log.i(TAG, "VPN STOPPED");
        }
    }

    @Override
    public void onDestroy() {
        desiredRunning = false;
        Thread thread;
        synchronized (this) {
            thread = supervisor;
        }
        if (thread != null) {
            thread.interrupt();
        }
        cleanupAllTransport();
        updateState(State.STOPPED, "");
        logStoppedOnce();
        if (networkMonitor != null) {
            networkMonitor.close();
            networkMonitor = null;
        }
        super.onDestroy();
    }

    @Override
    public void onRevoke() {
        requestStop();
        super.onRevoke();
    }
}
