package io.mbox.android;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.BroadcastReceiver;
import android.content.Context;
import android.system.OsConstants;
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
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
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

    public enum State { DISCONNECTED, CONNECTING, VERIFYING, CONNECTED, RECOVERING, FAILED, STOPPED }

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
    private static volatile String activeTransport = "";
    private static volatile String connectionPhase = "";

    private final AtomicBoolean stoppedLogged = new AtomicBoolean(true);
    private final AtomicLong underlyingChangeGeneration = new AtomicLong();
    private final TransportPolicy transportPolicy = new TransportPolicy();

    private volatile boolean desiredRunning;
    private volatile boolean cleaning;
    private Thread supervisor;
    private volatile Process mbox;
    private volatile int corePid;
    private ParcelFileDescriptor tun;
    private CronetBridge cronetBridge;
    private volatile QuicLabTransport quicLabTransport;
    private ConnectivityManager connectivityManager;
    private UnderlyingNetworkMonitor networkMonitor;
    private BroadcastReceiver wakeReceiver;

    public static State getState() {
        return state;
    }

    public static String getLastError() {
        return lastError;
    }

    public static String getActiveTransport() {
        return activeTransport;
    }
    public static String getConnectionPhase() {
        return connectionPhase;
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
        // Security invariant: restoration may be initiated by Android with a
        // null intent. Diagnose whether the service was actually recreated,
        // without logging profile URLs, account identifiers or credentials.
        Log.i(TAG, "VPN LIFECYCLE onCreate desired=" + desiredRunning
                + " always_on=" + isAlwaysOn()
                + " lockdown=" + isLockdownEnabled());
        wakeReceiver = new BroadcastReceiver() {
            @Override public void onReceive(Context context, Intent intent) {
                if (!desiredRunning) return;
                if (android.os.PowerManager.ACTION_DEVICE_IDLE_MODE_CHANGED.equals(intent.getAction())) {
                    android.os.PowerManager power = (android.os.PowerManager) getSystemService(POWER_SERVICE);
                    if (power != null && power.isDeviceIdleMode()) return;
                }
                // A signal requests a Noise ping; waking up is NOT proof of failure.
                Process process = mbox;
                if (process != null && process.isAlive()) {
                    int pid = corePid;
                    if (pid > 0) {
                        try { android.os.Process.sendSignal(pid, OsConstants.SIGUSR1); }
                        catch (RuntimeException failure) { Log.w(TAG, "Wake probe request failed"); }
                    }
                }
            }
        };
        IntentFilter wakeFilter = new IntentFilter();
        wakeFilter.addAction(Intent.ACTION_SCREEN_ON);
        wakeFilter.addAction(android.os.PowerManager.ACTION_DEVICE_IDLE_MODE_CHANGED);
        registerReceiver(wakeReceiver, wakeFilter);
        networkMonitor = new UnderlyingNetworkMonitor(connectivityManager, (previous, current) -> {
            if (!desiredRunning) {
                return;
            }
            underlyingChangeGeneration.incrementAndGet();
            updatePhase(current == null ? "Physical network lost; waiting for reconnection…"
                    : "Physical network changed; renewing MBox session…");
            Log.i(TAG, "NET stage=NETWORK code="
                    + (current == null ? "UNDERLYING_LOST" : "UNDERLYING_CHANGED"));
            restartCoreForNetworkChange();
        });
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? null : intent.getAction();
        // Only log the action category, never intent data or extras. These
        // logs distinguish an Android restart from a user-triggered start.
        String actionKind = ACTION_START.equals(action) ? "start"
                : ACTION_STOP.equals(action) ? "stop" : "system_or_null";
        Log.i(TAG, "VPN LIFECYCLE onStartCommand kind=" + actionKind
                + " flags=" + flags + " desired=" + desiredRunning);
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
            updateState(State.CONNECTING, "");
            updatePhase("Preparing protected VPN interface and HEV…");
            ensureFailClosedVpnInterface();

            while (desiredRunning) {
                if (!awaitUnderlyingNetworkIfNeeded()) {
                    break;
                }
                // Restore HEV if it died while preserving the full-route TUN.
                ensureFailClosedVpnInterface();
                long attemptNetworkGeneration = underlyingChangeGeneration.get();
                TransportPolicy.Mode requestedMode = QuicLabTransport.mode(this);
                TransportPolicy.Path path = transportPolicy.select(requestedMode,
                        attemptNetworkGeneration, SystemClock.elapsedRealtime());
                activeTransport = path.name();
                Log.i(TAG, "TRANSPORT mode=" + requestedMode + " selected=" + path);
                boolean immediateHttpsFallback = false;

                updateState(hasConnected ? State.RECOVERING : State.CONNECTING, "");
                updatePhase((hasConnected ? "Recovering" : "Connecting") +
                        " via " + path + "…");

                try {
                    Process process = startCoreTransport(path);
                    hasConnected = true;
                    long connectedAt = SystemClock.elapsedRealtime();

                    updateState(State.CONNECTED, "");
                    updatePhase("Encrypted remote session verified via " + path);
                    updateNotification("Connected via " + path + ": " + ProfileStore.profileName(this));
                    Log.i(TAG, "VPN CONNECTED");

                    // The Go core may remain alive after its native QUIC gateway dies.
                    // A 1-second bounded watch also observes HEV failure and stop requests.
                    while (desiredRunning && process.isAlive()) {
                        if (underlyingChangeGeneration.get() != attemptNetworkGeneration) {
                            throw new Exception("Underlying network changed");
                        }
                        if (quicLabTransport != null && !quicLabTransport.isAlive()) {
                            throw new Exception("QUIC gateway terminated");
                        }
                        if (!TProxyService.TProxyIsRunning()) {
                            throw new Exception("HEV tun2socks terminated");
                        }
                        if (process.waitFor(1, TimeUnit.SECONDS)) break;
                    }
                    if (!desiredRunning) break;
                    int code = process.isAlive() ? -1 : process.exitValue();
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
                    updatePhase("Core exited; restarting transport…");
                    immediateHttpsFallback = transportPolicy.failed(requestedMode, path,
                            attemptNetworkGeneration, SystemClock.elapsedRealtime());
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
                    updatePhase("Connection failed; replacing transport session…");
                    cleanupCoreTransport();
                    immediateHttpsFallback = transportPolicy.failed(requestedMode, path,
                            attemptNetworkGeneration, SystemClock.elapsedRealtime());
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

                if (immediateHttpsFallback) {
                    reconnectDelay = RECONNECT_MIN_MS;
                    Log.i(TAG, "TRANSPORT mode=AUTO code=QUIC_FAILED failover=HTTPS cooldown_ms="
                            + TransportPolicy.QUIC_RETRY_MS);
                    updatePhase("QUIC unavailable; switching to HTTPS…");
                    continue;
                }

                long actualDelay = jitteredReconnectDelay(reconnectDelay);
                updatePhase("Retry after " + formatDelay(actualDelay) + "…");
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
        updatePhase("Waiting for physical Wi-Fi/mobile network…");
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

        updatePhase("Physical network available; selecting transport…");
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

    private void updatePhase(String next) {
        // Only fixed, protocol-derived labels; never include endpoints or credential data.
        String safe = MBoxLogSanitizer.sanitize(next);
        if (safe.equals(connectionPhase)) return;
        connectionPhase = safe;
        if (desiredRunning) updateNotification(safe);
        Log.i(TAG, "VPN PHASE " + safe);
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

    private synchronized Process startCoreTransport(TransportPolicy.Path path) throws Exception {
        cleanupCoreTransport();

        if (!desiredRunning) {
            throw new InterruptedException("VPN stopped");
        }

        updatePhase("Checking imported MBox profile…");
        if (!ProfileStore.configFile(this).isFile()) {
            throw new Exception("Профиль MBox не импортирован");
        }

        String mboxBinary = getApplicationInfo().nativeLibraryDir + "/libmbox.so";
        Log.i(TAG, "MBox binary: " + mboxBinary);

        String endpointHost = ProfileStore.transportHost(this);
        String endpointUrl = ProfileStore.transportUrl(this);
        boolean networkDebug = ProfileStore.networkDebug(this);
        boolean quicLabMode = path == TransportPolicy.Path.QUIC;
        if (quicLabMode && !QuicLabTransport.configured(this)) {
            throw new SecurityException("QUIC is not configured in the imported profile");
        }
        String httpStack = ProfileStore.androidHttpStack(this);
        if (!quicLabMode && !"cronet".equals(httpStack)) {
            throw new SecurityException("Android VPN requires Cronet fail-closed transport; Go HTTP is diagnostic-only");
        }
        List<String> endpointAddresses = new ArrayList<>();
        if (networkDebug && !quicLabMode) {
            endpointAddresses = resolveEndpointAddresses(endpointHost);
            MBoxDnsDiagnostics.logDnsResult(endpointHost, endpointAddresses, true);
        } else {
            // Avoid an extra resolver query in the normal Cronet persona. Cronet owns
            // DNS/address racing on the explicitly bound physical Network.
            Log.i(TAG, "NET stage=DNS code=DNS_DELEGATED stack=cronet");
        }

        Network underlyingNetwork = networkMonitor != null
                ? networkMonitor.currentNetwork()
                : connectivityManager.getActiveNetwork();
        if (underlyingNetwork == null) {
            throw new Exception("No physical network available for encrypted transport");
        }
        updatePhase(quicLabMode ? "Starting protected QUIC gateway…"
                : "Starting Cronet HTTPS transport…");
        if (quicLabMode) quicLabTransport = new QuicLabTransport(this, underlyingNetwork);
        else cronetBridge = CronetBridge.start(this, endpointUrl, underlyingNetwork, networkDebug);
        updatePhase("Local transport ready; preparing encrypted MBox session…");

        CountDownLatch coreReady = new CountDownLatch(1);
        CountDownLatch remoteVerified = new CountDownLatch(1);
        // Persisted PSK stays Keystore-encrypted; plaintext exists only until
        // the core loads it. try-with-resources covers every failure path.
        try (ProfileStore.RuntimeMaterialization runtimeProfile = ProfileStore.materializeRuntimeConfig(this)) {
            if (quicLabMode) QuicLabTransport.rewriteRuntimeConfig(runtimeProfile.configFile);
            ProcessBuilder processBuilder = new ProcessBuilder(
                    mboxBinary, "-config", runtimeProfile.configFile.getAbsolutePath())
                    .redirectErrorStream(true);
            if (AppRuntime.udpLabEnabled(this)) {
                // Go requires a second explicit gate for lab-only UDP.
                processBuilder.environment().put("MBOX_DEVELOPER_MODE", "1");
            }
            if (!endpointAddresses.isEmpty()) {
                processBuilder.environment().put("MBOX_CONNECT_ADDRS", String.join(",", endpointAddresses));
                processBuilder.environment().put("MBOX_CONNECT_IP", endpointAddresses.get(0));
            }
            if (quicLabTransport != null) {
                processBuilder.environment().put("MBOX_QUIC_UNIX", quicLabTransport.unixPath());
            }
            if (cronetBridge != null) {
                processBuilder.environment().put("MBOX_HTTP_BRIDGE_ADDR", cronetBridge.address());
                processBuilder.environment().put("MBOX_HTTP_BRIDGE_TOKEN", cronetBridge.token());
            }
            processBuilder.environment().put("MBOX_DEVICE_ID", ProfileStore.installationDeviceId(this));
            updatePhase("Starting MBox core and remote authentication…");
            Process process = processBuilder.start();
            mbox = process;
            new Thread(() -> readCoreLog(process, coreReady, remoteVerified), "MBox-log").start();
            waitForCore(process, coreReady);
            updateState(State.VERIFYING, "");
            updatePhase("Checking authenticated remote " + path + " response…");
            waitForRemoteVerification(process, remoteVerified);
            if (!desiredRunning) {
                throw new InterruptedException("VPN stopped");
            }
            return process;
        }
    }

    private List<String> resolveEndpointAddresses(String host) throws Exception {
        ConnectivityManager manager = connectivityManager != null ? connectivityManager
                : (ConnectivityManager) getSystemService(CONNECTIVITY_SERVICE);
        Network network = networkMonitor != null ? networkMonitor.currentNetwork() : manager.getActiveNetwork();
        return MBoxDnsDiagnostics.resolveEndpointAddresses(network, host);
    }

    private static String safeMessage(Throwable error) {
        String value = error.getMessage();
        if (value == null || value.isEmpty()) {
            value = error.getClass().getSimpleName();
        }
        return value.length() > 180 ? value.substring(0, 180) : value;
    }

    private void readCoreLog(Process process, CountDownLatch ready, CountDownLatch verified) {
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(process.getInputStream()))) {
            String line;
            while ((line = reader.readLine()) != null) {
                final String pidPrefix = "NET stage=PROCESS code=CORE_PID pid=";
                if (line.startsWith(pidPrefix)) {
                    try {
                        int reported = Integer.parseInt(line.substring(pidPrefix.length()));
                        if (reported > 0 && process == mbox) corePid = reported;
                    } catch (NumberFormatException ignored) {
                        Log.w(TAG, "Invalid core PID announcement");
                    }
                    continue;
                }
                // Keep each authenticated liveness event out of verbose logcat.
                if (line.contains("NET stage=MBOX_HEALTH code=OK")) {
                    verified.countDown();
                    if (process == mbox && desiredRunning) {
                        updatePhase("Authenticated Noise heartbeat confirmed");
                    }
                    continue;
                }
                if (process == mbox && desiredRunning) {
                    if (line.contains("NET stage=MBOX_OPEN code=MBOX_OPEN_OK")) {
                        updatePhase("Remote MBox session opened; checking Noise…");
                    } else if (line.contains("NET stage=NOISE code=NOISE_OK")) {
                        updatePhase("Noise authenticated; starting local SOCKS…");
                    }
                }
                Log.i(TAG, "core: " + MBoxLogSanitizer.sanitize(line));
                if (line.contains("SOCKS listening on")) {
                    if (process == mbox && desiredRunning) {
                        updatePhase("Local SOCKS ready; verifying remote heartbeat…");
                    }
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

    private void waitForRemoteVerification(Process process, CountDownLatch verified) throws Exception {
        long deadline = SystemClock.elapsedRealtime() + 20_000;
        while (SystemClock.elapsedRealtime() < deadline) {
            if (!desiredRunning) throw new InterruptedException("VPN stopped");
            if (verified.await(200, TimeUnit.MILLISECONDS)) return;
            if (!process.isAlive()) throw new Exception("MBox core exited before remote verification");
            if (quicLabTransport != null && !quicLabTransport.isAlive()) {
                throw new Exception("QUIC gateway exited before remote verification");
            }
        }
        throw new Exception("Remote MBox session did not answer authenticated ping");
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
            corePid = 0;
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
            if (quicLabTransport != null) {
                try { quicLabTransport.close(); }
                catch (Throwable error) { Log.w(TAG, "QUIC laboratory stop failed", error); }
                quicLabTransport = null;
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
        activeTransport = "";
        connectionPhase = "";
    }

    private void logStoppedOnce() {
        if (stoppedLogged.compareAndSet(false, true)) {
            Log.i(TAG, "VPN STOPPED");
        }
    }

    @Override
    public void onDestroy() {
        Log.i(TAG, "VPN LIFECYCLE onDestroy");
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
        if (wakeReceiver != null) {
            try { unregisterReceiver(wakeReceiver); }
            catch (IllegalArgumentException ignored) { }
            wakeReceiver = null;
        }
        super.onDestroy();
    }

    @Override
    public void onRevoke() {
        requestStop();
        super.onRevoke();
    }
}
