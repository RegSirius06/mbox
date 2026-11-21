package io.mbox.android;

import java.util.function.LongUnaryOperator;

/** Supervisor-owned policy. Time is monotonic; probes never open a user session. */
final class TransportPolicy {
    enum Mode { AUTO, HTTPS, QUIC }
    enum Path { HTTPS, QUIC }

    static final long QUIC_RETRY_MS = 60_000;
    static final long MAX_RETRY_MS = 900_000;
    static final long CONFIRM_MS = 15_000;
    static final long STABLE_QUIC_MS = 120_000;
    private final LongUnaryOperator jitter;
    private long networkGeneration = Long.MIN_VALUE;
    private long retryQuicAfter;
    private long initialDelay = QUIC_RETRY_MS;
    private long maxDelay = MAX_RETRY_MS;
    private long nextDelay = QUIC_RETRY_MS;
    private long lastDelay;
    private boolean exponential = true;
    private boolean failback = true;
    private boolean confirming;
    private boolean startHttpsPending;
    private boolean initialHttpsTrial;

    // Deterministic clock/jitter inputs for the existing host Java harness.
    TransportPolicy() { this(delay -> delay); }
    TransportPolicy(LongUnaryOperator jitter) { this.jitter = jitter; }

    void configure(long initialMs, long maxMs, boolean exponential, boolean failback) {
        configure(initialMs, maxMs, exponential, failback, Path.QUIC);
    }

    void configure(long initialMs, long maxMs, boolean exponential, boolean failback, Path initialPath) {
        if (initialMs < 30_000 || initialMs > 300_000 || maxMs < initialMs || maxMs > 1_800_000)
            throw new IllegalArgumentException("Invalid QUIC retry intervals");
        this.initialDelay = initialMs;
        this.maxDelay = maxMs;
        this.exponential = exponential;
        this.failback = failback;
        networkGeneration = Long.MIN_VALUE;
        startHttpsPending = initialPath == Path.HTTPS;
        initialHttpsTrial = false;
        resetRetry();
    }

    Path select(Mode mode, long generation, long nowMs) {
        if (mode == Mode.HTTPS) return Path.HTTPS;
        if (mode == Mode.QUIC) return Path.QUIC;
        resetOnNetworkChange(generation);
        // Initial preference is consumed once per explicit VPN start, not on failback.
        initialHttpsTrial = startHttpsPending;
        startHttpsPending = false;
        if (initialHttpsTrial) return Path.HTTPS;
        return nowMs < retryQuicAfter ? Path.HTTPS : Path.QUIC;
    }

    /** Arm probes only after the intentional HTTPS start has authenticated. */
    void connected(Mode mode, Path path, long generation, long nowMs) {
        if (!initialHttpsTrial || mode != Mode.AUTO || path != Path.HTTPS
                || generation != networkGeneration) return;
        initialHttpsTrial = false;
        if (!failback) return;
        postpone(nowMs);
        // Starting on HTTPS is not a QUIC failure: keep the first retry interval.
        nextDelay = initialDelay;
    }

    /** A failed Auto QUIC attempt must retry over HTTPS immediately, once. */
    boolean failed(Mode mode, Path path, long generation, long nowMs) {
        if (mode != Mode.AUTO || path != Path.QUIC) return false;
        resetOnNetworkChange(generation);
        postpone(nowMs);
        return true;
    }

    boolean probeDue(Mode mode, Path path, long generation, long nowMs) {
        return failback && mode == Mode.AUTO && path == Path.HTTPS
                && networkGeneration == generation && retryQuicAfter != 0 && nowMs >= retryQuicAfter;
    }

    /** Requires two successful independent QUIC/TLS/HTTP3 checks. */
    boolean probeResult(long generation, long nowMs, boolean successful) {
        if (!failback || generation != networkGeneration || retryQuicAfter == 0 || nowMs < retryQuicAfter)
            return false;
        if (!successful) {
            postpone(nowMs);
            return false;
        }
        if (!confirming) {
            confirming = true;
            retryQuicAfter = deadline(nowMs, Math.max(CONFIRM_MS, Math.min(20_000, jitter.applyAsLong(CONFIRM_MS))));
            return false;
        }
        confirming = false;
        retryQuicAfter = 0;
        lastDelay = 0;
        // Keep the increased backoff until a real QUIC session is stable.
        return true;
    }

    void stableQuic(long generation) {
        if (networkGeneration == generation) resetRetry();
    }

    long retryAt() { return retryQuicAfter; }
    long lastDelay() { return lastDelay; }
    boolean enabled() { return failback; }
    boolean confirming() { return confirming; }

    private void postpone(long nowMs) {
        confirming = false;
        // With failback disabled, preserve the old fixed cooldown behavior.
        long base = failback ? nextDelay : QUIC_RETRY_MS;
        lastDelay = failback ? Math.min(maxDelay, Math.max(30_000, jitter.applyAsLong(base))) : QUIC_RETRY_MS;
        retryQuicAfter = deadline(nowMs, lastDelay);
        if (failback && exponential) nextDelay = Math.min(maxDelay, nextDelay * 2);
    }

    private static long deadline(long nowMs, long delay) {
        return nowMs > Long.MAX_VALUE - delay ? Long.MAX_VALUE : nowMs + delay;
    }

    private void resetRetry() {
        retryQuicAfter = 0;
        nextDelay = initialDelay;
        lastDelay = 0;
        confirming = false;
    }

    private void resetOnNetworkChange(long generation) {
        if (networkGeneration != generation) {
            networkGeneration = generation;
            resetRetry();
        }
    }
}
