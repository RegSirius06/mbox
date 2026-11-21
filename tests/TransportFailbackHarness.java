package io.mbox.android;

/** Tests the real production policy using monotonic simulated time, not sleeps. */
public final class TransportFailbackHarness {
    private static final TransportPolicy.Mode AUTO = TransportPolicy.Mode.AUTO;
    private static final TransportPolicy.Path QUIC = TransportPolicy.Path.QUIC;
    private static final TransportPolicy.Path HTTPS = TransportPolicy.Path.HTTPS;
    private static int assertions;

    private static void eq(Object expected, Object actual) {
        assertions++;
        if (!expected.equals(actual)) throw new AssertionError(expected + " != " + actual);
    }

    private static void confirmationAndFlapping() {
        TransportPolicy p = new TransportPolicy();
        eq(QUIC, p.select(AUTO, 1, 100));
        eq(false, p.probeDue(AUTO, QUIC, 1, 100_000));
        eq(true, p.failed(AUTO, QUIC, 1, 100));
        eq(HTTPS, p.select(AUTO, 1, 100));
        eq(false, p.probeDue(AUTO, HTTPS, 1, 60_099));
        eq(true, p.probeDue(AUTO, HTTPS, 1, 60_100));
        eq(false, p.probeResult(1, 61_000, true));
        eq(true, p.confirming());
        eq(76_000L, p.retryAt());
        eq(false, p.probeResult(1, 75_999, true));
        eq(true, p.probeResult(1, 76_100, true));
        eq(QUIC, p.select(AUTO, 1, 76_100));
        eq(false, p.probeDue(AUTO, HTTPS, 1, 76_100));
        // A failed real MBox session after successful HTTP/3 checks must back off.
        eq(true, p.failed(AUTO, QUIC, 1, 76_101));
        eq(120_000L, p.lastDelay());
        eq(HTTPS, p.select(AUTO, 1, 76_101));
        eq(false, p.probeDue(AUTO, HTTPS, 1, 196_100));
        eq(false, p.probeResult(1, 196_101, false));
        eq(240_000L, p.lastDelay());
        eq(false, p.confirming());
        eq(false, p.probeResult(1, 436_101, true));
        eq(false, p.probeResult(1, 451_101, false));
        eq(480_000L, p.lastDelay());
        eq(false, p.confirming());
        eq(QUIC, p.select(AUTO, 2, 451_102));
        eq(false, p.probeResult(1, 1_000_000, true));
        eq(true, p.failed(AUTO, QUIC, 2, 451_103));
        eq(60_000L, p.lastDelay());
        p.stableQuic(2);
        eq(true, p.failed(AUTO, QUIC, 2, 600_000));
        eq(60_000L, p.lastDelay());
    }

    private static void capsAndManualModes() {
        TransportPolicy p = new TransportPolicy(delay -> delay * 12 / 10);
        p.configure(30_000, 70_000, true, true);
        eq(true, p.failed(AUTO, QUIC, 10, 1_000));
        eq(36_000L, p.lastDelay());
        eq(false, p.probeResult(10, 37_000, false));
        eq(70_000L, p.lastDelay());
        eq(false, p.probeResult(10, 107_000, false));
        eq(70_000L, p.lastDelay());
        eq(false, p.probeDue(TransportPolicy.Mode.HTTPS, HTTPS, 10, 1_000_000));
        eq(false, p.probeDue(TransportPolicy.Mode.QUIC, HTTPS, 10, 1_000_000));
        eq(false, p.failed(TransportPolicy.Mode.QUIC, QUIC, 10, 1_000_000));
        eq(QUIC, p.select(TransportPolicy.Mode.QUIC, 10, 1));
        eq(HTTPS, p.select(TransportPolicy.Mode.HTTPS, 10, 1_000_000));
        p.configure(30_000, 70_000, false, true);
        eq(true, p.failed(AUTO, QUIC, 10, 1_000));
        eq(false, p.probeResult(10, 37_000, false));
        eq(36_000L, p.lastDelay());
        p.configure(30_000, 70_000, true, false);
        eq(true, p.failed(AUTO, QUIC, 10, 1_000));
        eq(60_000L, p.lastDelay());
        eq(false, p.probeDue(AUTO, HTTPS, 10, 1_000_000));
        eq(false, p.probeResult(10, 1_000_000, true));
        eq(QUIC, p.select(AUTO, 10, 61_000));
        try { p.configure(0, 1_800_000, true, true); throw new AssertionError("zero interval accepted"); }
        catch (IllegalArgumentException expected) { assertions++; }
        try { p.configure(60_000, 10_000, true, true); throw new AssertionError("invalid maximum accepted"); }
        catch (IllegalArgumentException expected) { assertions++; }
    }

    private static void httpsFirst() {
        TransportPolicy p = new TransportPolicy();
        p.configure(30_000, 900_000, true, true, HTTPS);
        eq(HTTPS, p.select(AUTO, 1, 100));
        eq(false, p.probeDue(AUTO, HTTPS, 1, 100_000));
        p.connected(AUTO, HTTPS, 1, 1_000);
        eq(31_000L, p.retryAt());
        p.connected(AUTO, HTTPS, 1, 2_000); // must not rearm
        eq(31_000L, p.retryAt());
        eq(false, p.probeDue(AUTO, HTTPS, 1, 30_999));
        eq(true, p.probeDue(AUTO, HTTPS, 1, 31_000));
        eq(false, p.probeResult(1, 31_000, true));
        eq(true, p.probeResult(1, 46_000, true));
        eq(QUIC, p.select(AUTO, 1, 46_000));
        eq(true, p.failed(AUTO, QUIC, 1, 46_001));
        eq(30_000L, p.lastDelay());
        eq(HTTPS, p.select(AUTO, 1, 46_001));
        eq(QUIC, p.select(AUTO, 2, 46_002));

        // HTTPS itself unavailable: the next attempt can use QUIC.
        p.configure(30_000, 900_000, true, true, HTTPS);
        eq(HTTPS, p.select(AUTO, 3, 100));
        eq(false, p.failed(AUTO, HTTPS, 3, 200));
        eq(QUIC, p.select(AUTO, 3, 300));

        // No background promotion when failback is off; forced modes win.
        p.configure(30_000, 900_000, true, false, HTTPS);
        eq(HTTPS, p.select(AUTO, 4, 100));
        p.connected(AUTO, HTTPS, 4, 200);
        eq(false, p.probeDue(AUTO, HTTPS, 4, 100_000));
        eq(0L, p.retryAt());
        p.configure(30_000, 900_000, true, true, HTTPS);
        eq(QUIC, p.select(TransportPolicy.Mode.QUIC, 5, 100));
        eq(HTTPS, p.select(TransportPolicy.Mode.HTTPS, 5, 100));
        p.configure(30_000, 900_000, true, true, QUIC);
        eq(QUIC, p.select(AUTO, 6, 100));

        // First failed reachability probe does not inherit a fictitious failure.
        p.configure(30_000, 900_000, true, true, HTTPS);
        eq(HTTPS, p.select(AUTO, 7, 100));
        p.connected(AUTO, HTTPS, 7, 200);
        eq(false, p.probeResult(7, 30_200, false));
        eq(30_000L, p.lastDelay());
        eq(false, p.probeResult(7, 60_200, false));
        eq(60_000L, p.lastDelay());
    }

    public static void main(String[] args) {
        confirmationAndFlapping();
        capsAndManualModes();
        httpsFirst();
        System.out.println("TransportFailback OK: " + assertions + " assertions");
    }
}
