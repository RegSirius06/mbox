package io.mbox.android;

/** Runs on the host JDK, without Android SDK or a live network. */
public final class TransportPolicyHarness {
    private static void eq(Object expected, Object actual) {
        if (!expected.equals(actual)) throw new AssertionError(expected + " != " + actual);
    }

    private static void autoFallbackAndCooldown() {
        TransportPolicy p = new TransportPolicy();
        eq(TransportPolicy.Path.QUIC, p.select(TransportPolicy.Mode.AUTO, 1, 100));
        if (!p.failed(TransportPolicy.Mode.AUTO, TransportPolicy.Path.QUIC, 1, 120))
            throw new AssertionError("QUIC must fall back at once");
        eq(TransportPolicy.Path.HTTPS, p.select(TransportPolicy.Mode.AUTO, 1, 120));
        eq(TransportPolicy.Path.HTTPS, p.select(TransportPolicy.Mode.AUTO, 1, 60_119));
        eq(TransportPolicy.Path.QUIC, p.select(TransportPolicy.Mode.AUTO, 1, 60_120));
        // If QUIC fails again it must not spin QUIC retries.
        p.failed(TransportPolicy.Mode.AUTO, TransportPolicy.Path.QUIC, 1, 60_121);
        eq(TransportPolicy.Path.HTTPS, p.select(TransportPolicy.Mode.AUTO, 1, 60_122));
    }

    private static void networkChangesResetCooldown() {
        TransportPolicy p = new TransportPolicy();
        p.failed(TransportPolicy.Mode.AUTO, TransportPolicy.Path.QUIC, 10, 1000);
        eq(TransportPolicy.Path.HTTPS, p.select(TransportPolicy.Mode.AUTO, 10, 1001));
        eq(TransportPolicy.Path.QUIC, p.select(TransportPolicy.Mode.AUTO, 11, 1001));
        eq(TransportPolicy.Path.QUIC, p.select(TransportPolicy.Mode.AUTO, 11, 1002));
    }

    private static void forcedModesNeverSwitch() {
        TransportPolicy p = new TransportPolicy();
        eq(TransportPolicy.Path.HTTPS, p.select(TransportPolicy.Mode.HTTPS, 1, 0));
        if (p.failed(TransportPolicy.Mode.HTTPS, TransportPolicy.Path.HTTPS, 1, 0))
            throw new AssertionError("manual HTTPS must never fail over to direct UDP");
        eq(TransportPolicy.Path.HTTPS, p.select(TransportPolicy.Mode.HTTPS, 1, 123456));
        eq(TransportPolicy.Path.QUIC, p.select(TransportPolicy.Mode.QUIC, 1, 0));
        if (p.failed(TransportPolicy.Mode.QUIC, TransportPolicy.Path.QUIC, 1, 1))
            throw new AssertionError("manual QUIC must not silently switch paths");
        eq(TransportPolicy.Path.QUIC, p.select(TransportPolicy.Mode.QUIC, 1, 2));
        // An HTTP failure in Auto must not cause automatic direct UDP retries.
        if (p.failed(TransportPolicy.Mode.AUTO, TransportPolicy.Path.HTTPS, 1, 2))
            throw new AssertionError("HTTPS failures use normal retry backoff");
    }

    private static void overflow() {
        TransportPolicy p = new TransportPolicy();
        p.failed(TransportPolicy.Mode.AUTO, TransportPolicy.Path.QUIC, 1, Long.MAX_VALUE - 10);
        eq(TransportPolicy.Path.HTTPS, p.select(TransportPolicy.Mode.AUTO, 1, Long.MAX_VALUE - 9));
    }

    public static void main(String[] args) {
        autoFallbackAndCooldown();
        networkChangesResetCooldown();
        forcedModesNeverSwitch();
        overflow();
        System.out.println("TransportPolicy OK");
    }
}
