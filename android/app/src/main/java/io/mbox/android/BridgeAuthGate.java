package io.mbox.android;

import java.io.DataInputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;

/**
 * Authenticate the fixed-size prefix of the loopback bridge wire protocol.
 * No request path, headers or body is parsed until this gate has passed.
 * Package-private and Android-independent so the actual parser can be tested
 * on a host JDK without reimplementing the protocol in Python.
 */
final class BridgeAuthGate {
    private static final int MAX_TOKEN_BYTES = 512;

    static final class InvalidTokenException extends IOException {
        InvalidTokenException() { super("invalid bridge token"); }
    }

    static void requireToken(DataInputStream in, int expectedMagic, String expectedToken)
            throws IOException {
        if (in.readInt() != expectedMagic) {
            throw new IOException("invalid bridge request magic");
        }
        int length = in.readUnsignedShort();
        if (length > MAX_TOKEN_BYTES) {
            throw new InvalidTokenException();
        }
        byte[] provided = new byte[length];
        in.readFully(provided);
        byte[] expected = expectedToken.getBytes(StandardCharsets.UTF_8);
        int difference = provided.length ^ expected.length;
        // Compare without exiting early on the first wrong byte.
        for (int i = 0; i < MAX_TOKEN_BYTES; i++) {
            int a = i < provided.length ? provided[i] & 0xff : 0;
            int b = i < expected.length ? expected[i] & 0xff : 0;
            difference |= a ^ b;
        }
        if (difference != 0) {
            throw new InvalidTokenException();
        }
    }

    private BridgeAuthGate() { }
}
