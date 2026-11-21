package io.mbox.android;

import android.content.Context;
import android.net.Network;
import android.os.PowerManager;
import android.os.SystemClock;
import android.util.Log;

import java.util.concurrent.atomic.AtomicLong;

/** Uses the existing supervisor watch; no executor, alarm or extra WakeLock. */
final class QuicFailback implements AutoCloseable {
    private static final long PROBE_TIMEOUT_MS = 9_000;
    private static final AtomicLong attempts = new AtomicLong();
    private static final AtomicLong failures = new AtomicLong();
    private static final AtomicLong promotions = new AtomicLong();
    private static volatile long lastSuccessAt;
    private static volatile long nextCheckAt;
    private static volatile long backoff;
    private static volatile String status = "inactive";

    private final Context context;
    private final TransportPolicy policy;
    private final TransportPolicy.Mode mode;
    private final TransportPolicy.Path path;
    private final long generation;
    private final PowerManager power;
    private QuicLabTransport probe;
    private long deadline;
    private boolean stable;

    QuicFailback(Context context, TransportPolicy policy, TransportPolicy.Mode mode,
                TransportPolicy.Path path, long generation) {
        this.context = context;
        this.policy = policy;
        this.mode = mode;
        this.path = path;
        this.generation = generation;
        power = (PowerManager) context.getSystemService(Context.POWER_SERVICE);
        if (eligible()) waiting();
    }

    private boolean eligible() {
        return mode == TransportPolicy.Mode.AUTO && path == TransportPolicy.Path.HTTPS && policy.enabled();
    }

    boolean tick(UnderlyingNetworkMonitor monitor, long connectedAt) throws InterruptedException {
        if (Thread.currentThread().isInterrupted()) throw new InterruptedException();
        long now = SystemClock.elapsedRealtime();
        if (path == TransportPolicy.Path.QUIC) {
            if (!stable && now - connectedAt >= TransportPolicy.STABLE_QUIC_MS) {
                policy.stableQuic(generation);
                stable = true;
            }
            return false;
        }
        if (!eligible()) return false;
        // Avoid checking Android network state unless a scheduled probe is due.
        if (probe == null && !policy.probeDue(mode, path, generation, now)) return false;
        // No new network work during Doze, and do not promote from a probe
        // that was interrupted by device idle. Keep the due time unchanged.
        if (power == null || power.isDeviceIdleMode()) {
            if (probe != null) { closeProbe(); waiting(); }
            return false;
        }
        if (probe != null) {
            if (probe.isAlive() && now < deadline) return false;
            boolean ok = now < deadline && probe.probeSucceeded();
            closeProbe();
            if (Thread.currentThread().isInterrupted()) throw new InterruptedException();
            return finish(ok);
        }
        Network network = monitor == null ? null : monitor.currentNetwork();
        if (network == null) return false;
        attempts.incrementAndGet();
        status = "checking";
        nextCheckAt = 0;
        try {
            probe = QuicLabTransport.startProbe(context, network);
            deadline = SystemClock.elapsedRealtime() + PROBE_TIMEOUT_MS;
            if (Thread.currentThread().isInterrupted()) throw new InterruptedException();
        } catch (InterruptedException interrupted) {
            closeProbe();
            throw interrupted;
        } catch (Exception unavailable) {
            closeProbe();
            return finish(false);
        }
        return false;
    }

    private boolean finish(boolean ok) {
        long now = SystemClock.elapsedRealtime();
        if (ok) lastSuccessAt = now;
        else failures.incrementAndGet();
        boolean promote = policy.probeResult(generation, now, ok);
        Log.i("MBox", "TRANSPORT probe=" + (ok ? "H3_OK" : "FAILED") + " promote=" + promote);
        if (promote) {
            promotions.incrementAndGet();
            status = "switch requested";
            nextCheckAt = 0;
        } else {
            waiting();
        }
        return promote;
    }

    private void waiting() {
        status = policy.confirming() ? "confirming" : "waiting";
        nextCheckAt = policy.retryAt();
        backoff = policy.lastDelay();
    }

    private void closeProbe() {
        if (probe != null) {
            try { probe.close(); }
            finally { probe = null; }
        }
    }

    @Override public void close() {
        try { closeProbe(); }
        finally {
            status = "inactive";
            nextCheckAt = 0;
            backoff = 0;
        }
    }

    static String diagnostics(Context context) {
        long now = SystemClock.elapsedRealtime();
        long next = nextCheckAt;
        long last = lastSuccessAt;
        boolean ru = UiLanguage.russian(context);
        String current = status;
        if (ru) {
            switch (current) {
                case "checking": current = "проверка"; break;
                case "confirming": current = "подтверждение"; break;
                case "waiting": current = "ожидание"; break;
                case "switch requested": current = "запрос переключения"; break;
                default: current = "неактивен";
            }
        }
        return "Failback: " + current
                + (ru ? "\nДо проверки, с: " : "\nNext check, s: ") + (next == 0 ? "—" : Long.toString(Math.max(0, (next - now + 999) / 1000)))
                + (ru ? "\nBackoff, с: " : "\nBackoff, s: ") + (backoff == 0 ? "—" : Long.toString(backoff / 1000))
                + (ru ? "\nПоследний успех HTTP/3, с назад: " : "\nLast H3 success, s ago: ") + (last == 0 ? "—" : Long.toString(Math.max(0, (now - last) / 1000)))
                + (ru ? "\nПроверки / ошибки / запросы failback: " : "\nChecks / failures / failback requests: ")
                + attempts.get() + " / " + failures.get() + " / " + promotions.get();
    }
}
