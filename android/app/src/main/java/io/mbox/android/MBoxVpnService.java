package io.mbox.android;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Intent;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.VpnService;
import android.os.ParcelFileDescriptor;
import android.util.Log;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStreamReader;
import java.net.Inet4Address;
import java.net.InetAddress;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

import hev.htproxy.TProxyService;

/** Owns the Android TUN, HEV tun2socks and the native MBox client process. */
public class MBoxVpnService extends VpnService {
    public static final String ACTION_START = "io.mbox.android.START";
    public static final String ACTION_STOP = "io.mbox.android.STOP";

    public enum State { STOPPED, CONNECTING, RECONNECTING, RUNNING, ERROR }

    private static final String TAG = "MBox";
    private static final String CHANNEL_ID = "mbox-vpn";
    private static final int NOTIFICATION_ID = 1;

    private static final long RECONNECT_MIN_MS = 1_000;
    private static final long RECONNECT_MAX_MS = 30_000;

    private static volatile State state = State.STOPPED;
    private static volatile String lastError = "";

    private final AtomicBoolean stoppedLogged = new AtomicBoolean(true);

    private volatile boolean desiredRunning;
    private volatile boolean cleaning;
    private Thread supervisor;
    private Process mbox;
    private ParcelFileDescriptor tun;

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
    }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? null : intent.getAction();

        if (ACTION_STOP.equals(action)) {
            requestStop();
            return START_NOT_STICKY;
        }

        if (!ACTION_START.equals(action)) {
            return desiredRunning ? START_STICKY : START_NOT_STICKY;
        }

        desiredRunning = true;
        stoppedLogged.set(false);
        ensureForeground("Connecting…");

        synchronized (this) {
            if (supervisor == null || !supervisor.isAlive()) {
                supervisor = new Thread(this::runSupervisor, "MBox-supervisor");
                supervisor.start();
            }
        }

        return START_STICKY;
    }

    private void runSupervisor() {
        long reconnectDelay = RECONNECT_MIN_MS;
        boolean firstAttempt = true;

        try {
            while (desiredRunning) {
                updateState(firstAttempt ? State.CONNECTING : State.RECONNECTING, "");
                updateNotification(firstAttempt ? "Connecting…" : "Reconnecting…");

                try {
                    Process process = startTunnel();
                    firstAttempt = false;
                    reconnectDelay = RECONNECT_MIN_MS;

                    int code = process.waitFor();
                    if (!desiredRunning || process != mbox) {
                        break;
                    }

                    if (code == 0) {
                        Log.i(TAG, "MBox core exited: 0");
                    } else {
                        Log.e(TAG, "MBox core exited unexpectedly: " + code);
                    }

                    cleanupTransport();
                    if (!desiredRunning) {
                        break;
                    }
                    updateState(State.RECONNECTING, "MBox core завершился: " + code);
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                    break;
                } catch (Throwable error) {
                    if (!desiredRunning) {
                        break;
                    }
                    String message = error.getMessage() == null ? error.toString() : error.getMessage();
                    Log.w(TAG, "VPN tunnel attempt failed", error);
                    updateState(State.RECONNECTING, message);
                    cleanupTransport();
                }

                if (!desiredRunning) {
                    break;
                }

                updateNotification("Reconnecting in " + formatDelay(reconnectDelay) + "…");
                Log.i(TAG, "RECONNECT retry in " + reconnectDelay + "ms");

                try {
                    Thread.sleep(reconnectDelay);
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                    break;
                }

                reconnectDelay = Math.min(reconnectDelay * 2, RECONNECT_MAX_MS);
            }
        } finally {
            cleanupTransport();
            synchronized (this) {
                if (Thread.currentThread() == supervisor) {
                    supervisor = null;
                }
            }

            if (!desiredRunning) {
                updateState(State.STOPPED, "");
                logStoppedOnce();
                stopForeground(STOP_FOREGROUND_REMOVE);
                stopSelf();
            }
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
        startForeground(NOTIFICATION_ID, notification);
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
        lastError = error == null ? "" : error;
    }

    private synchronized Process startTunnel() throws Exception {
        cleanupTransport();

        if (!desiredRunning) {
            throw new InterruptedException("VPN stopped");
        }

        File clientConfig = ProfileStore.configFile(this);
        if (!clientConfig.isFile()) {
            throw new Exception("Профиль MBox не импортирован");
        }

        String mboxBinary = getApplicationInfo().nativeLibraryDir + "/libmbox.so";
        Log.i(TAG, "MBox binary: " + mboxBinary);

        String endpointHost = ProfileStore.transportHost(this);
        String endpointIp = resolveEndpointIp(endpointHost);
        Log.i(TAG, "Endpoint DNS: " + endpointHost + " -> " + endpointIp);

        CountDownLatch coreReady = new CountDownLatch(1);
        ProcessBuilder processBuilder = new ProcessBuilder(
                mboxBinary, "-config", clientConfig.getAbsolutePath())
                .redirectErrorStream(true);
        processBuilder.environment().put("MBOX_CONNECT_IP", endpointIp);
        Process process = processBuilder.start();
        mbox = process;

        new Thread(() -> readCoreLog(process, coreReady), "MBox-log").start();
        waitForCore(process, coreReady);

        if (!desiredRunning) {
            throw new InterruptedException("VPN stopped");
        }

        Builder builder = new Builder()
                .setSession("MBox")
                .setMtu(AppRuntime.MTU)
                .addAddress(AppRuntime.TUN_IPV4, 32)
                .addRoute("0.0.0.0", 0)
                .addDnsServer(AppRuntime.MAP_DNS);

        builder.addDisallowedApplication(getPackageName());

        tun = builder.establish();
        if (tun == null) {
            throw new Exception("Android не создал VPN TUN");
        }

        File hevConfig = new File(getFilesDir(), "hev.yml");
        try (FileOutputStream out = new FileOutputStream(hevConfig, false)) {
            out.write(AppRuntime.hevConfig().getBytes(StandardCharsets.UTF_8));
            out.getFD().sync();
        }

        if (!TProxyService.TProxyStartService(hevConfig.getAbsolutePath(), tun.getFd())) {
            throw new Exception("HEV tun2socks не запустился");
        }

        updateState(State.RUNNING, "");
        updateNotification("Connected: " + ProfileStore.profileName(this));
        Log.i(TAG, "VPN STARTED");
        return process;
    }

    private String resolveEndpointIp(String host) throws Exception {
        ConnectivityManager manager =
                (ConnectivityManager) getSystemService(CONNECTIVITY_SERVICE);
        Network network = manager.getActiveNetwork();
        if (network == null) {
            throw new Exception("Нет активной физической сети для DNS");
        }

        InetAddress[] addresses = network.getAllByName(host);
        if (addresses.length == 0) {
            throw new Exception("DNS не вернул адрес для " + host);
        }

        // Prefer IPv4 because the current public MBox path is IPv4-tested.
        // Fall back to IPv6 when a network/domain is IPv6-only.
        for (InetAddress address : addresses) {
            if (address instanceof Inet4Address) {
                return address.getHostAddress();
            }
        }
        return addresses[0].getHostAddress();
    }

    private void readCoreLog(Process process, CountDownLatch ready) {
        try (BufferedReader reader = new BufferedReader(new InputStreamReader(process.getInputStream()))) {
            String line;
            while ((line = reader.readLine()) != null) {
                Log.i(TAG, "core: " + line);
                if (line.contains("SOCKS listening on")) {
                    ready.countDown();
                }
            }
        } catch (Exception error) {
            if (!cleaning && desiredRunning && process == mbox) {
                Log.w(TAG, "core log reader stopped", error);
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
        desiredRunning = false;

        Thread thread;
        synchronized (this) {
            thread = supervisor;
        }
        if (thread != null) {
            thread.interrupt();
        }

        cleanupTransport();
        updateState(State.STOPPED, "");
        logStoppedOnce();
        stopForeground(STOP_FOREGROUND_REMOVE);
        stopSelf();
    }

    private synchronized void cleanupTransport() {
        cleaning = true;
        try {
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
        } finally {
            cleaning = false;
        }
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
        cleanupTransport();
        updateState(State.STOPPED, "");
        logStoppedOnce();
        super.onDestroy();
    }

    @Override
    public void onRevoke() {
        requestStop();
        super.onRevoke();
    }
}
