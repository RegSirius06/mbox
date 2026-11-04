package io.mbox.android;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;

/** Executable regression checks for the real Java pre-auth bridge parser. */
public final class BridgeAuthGateHarness {
    private static final int MAGIC = 0x4d425231;
    private static final String TOKEN = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";

    private static void check(boolean ok, String message) {
        if (!ok) throw new AssertionError(message);
    }

    private static byte[] message(String token, int suffixBytes) throws Exception {
        ByteArrayOutputStream bytes = new ByteArrayOutputStream();
        DataOutputStream out = new DataOutputStream(bytes);
        out.writeInt(MAGIC);
        byte[] t = token.getBytes(StandardCharsets.UTF_8);
        out.writeShort(t.length);
        out.write(t);
        byte[] suffix = new byte[suffixBytes];
        Arrays.fill(suffix, (byte) 0x7f);
        out.write(suffix);
        return bytes.toByteArray();
    }

    private static void malformedTokenNeverConsumesRequestBody() throws Exception {
        byte[] bytes = message(TOKEN.substring(0, 63) + "0", 8 * 1024 * 1024);
        ByteArrayInputStream raw = new ByteArrayInputStream(bytes);
        DataInputStream in = new DataInputStream(raw);
        try {
            BridgeAuthGate.requireToken(in, MAGIC, TOKEN);
            throw new AssertionError("invalid token accepted");
        } catch (BridgeAuthGate.InvalidTokenException expected) {
            check(raw.available() == 8 * 1024 * 1024,
                    "unauthenticated peer consumed large request body");
        }
    }

    private static void correctTokenPassesWithoutConsumingBody() throws Exception {
        byte[] bytes = message(TOKEN, 32);
        ByteArrayInputStream raw = new ByteArrayInputStream(bytes);
        BridgeAuthGate.requireToken(new DataInputStream(raw), MAGIC, TOKEN);
        check(raw.available() == 32, "authenticated parser ate request payload");
    }

    private static void oversizedTokenRejectedBeforeAllocation() throws Exception {
        byte[] bytes = {0x4d, 0x42, 0x52, 0x31, 0x02, 0x01}; // token len = 513
        try {
            BridgeAuthGate.requireToken(new DataInputStream(new ByteArrayInputStream(bytes)), MAGIC, TOKEN);
            throw new AssertionError("oversized token accepted");
        } catch (BridgeAuthGate.InvalidTokenException expected) {
            // No token bytes were needed.
        }
    }

    private static void wrongMagicRejected() throws Exception {
        byte[] bytes = message(TOKEN, 0);
        bytes[3] ^= 1;
        try {
            BridgeAuthGate.requireToken(new DataInputStream(new ByteArrayInputStream(bytes)), MAGIC, TOKEN);
            throw new AssertionError("wrong magic accepted");
        } catch (java.io.IOException expected) { }
    }

    public static void main(String[] args) throws Exception {
        malformedTokenNeverConsumesRequestBody();
        correctTokenPassesWithoutConsumingBody();
        oversizedTokenRejectedBeforeAllocation();
        wrongMagicRejected();
        System.out.println("PASS: Cronet bridge pre-auth gate / bounded input");
    }
}
