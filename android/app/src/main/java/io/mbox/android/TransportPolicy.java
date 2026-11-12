package io.mbox.android;

/** One active MBox session at a time; Auto fails over without opening the VPN TUN. */
final class TransportPolicy {
    enum Mode { AUTO, HTTPS, QUIC }
    enum Path { HTTPS, QUIC }

    static final long QUIC_RETRY_MS = 60_000;

    private long networkGeneration = Long.MIN_VALUE;
    private long retryQuicAfter;

    Path select(Mode mode, long generation, long nowMs) {
        if (mode == Mode.HTTPS) return Path.HTTPS;
        if (mode == Mode.QUIC) return Path.QUIC;
        resetOnNetworkChange(generation);
        return nowMs < retryQuicAfter ? Path.HTTPS : Path.QUIC;
    }

    /** A failed Auto QUIC attempt must retry over HTTPS immediately, once. */
    boolean failed(Mode mode, Path path, long generation, long nowMs) {
        if (mode != Mode.AUTO || path != Path.QUIC) return false;
        resetOnNetworkChange(generation);
        // uptime is monotonic. Guard overflow rather than accidentally disabling cooldown.
        retryQuicAfter = nowMs > Long.MAX_VALUE - QUIC_RETRY_MS
                ? Long.MAX_VALUE : nowMs + QUIC_RETRY_MS;
        return true;
    }

    private void resetOnNetworkChange(long generation) {
        if (networkGeneration != generation) {
            networkGeneration = generation;
            retryQuicAfter = 0;
        }
    }
}
