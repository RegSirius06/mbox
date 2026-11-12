package io.mbox.android;

import android.net.LocalServerSocket;
import android.net.LocalSocket;
import android.net.Network;
import android.os.Process;
import android.system.Os;

import java.io.Closeable;
import java.io.DataInputStream;
import java.io.FileDescriptor;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;

/** App-private FD broker. No QUIC packet leaves before Network.bindSocket succeeds. */
final class QuicSocketProtector implements Closeable {
    private final LocalServerSocket listener;
    private final String name;
    private final String token;
    private final Network physicalNetwork;
    private final Thread worker;
    private volatile boolean closed;

    QuicSocketProtector(Network network) throws Exception {
        if (network == null) throw new IOException("No physical Network for QUIC");
        physicalNetwork = network;
        byte[] random = new byte[20];
        new SecureRandom().nextBytes(random);
        StringBuilder suffix = new StringBuilder();
        for (byte b : random) suffix.append(String.format(java.util.Locale.ROOT, "%02x", b & 0xff));
        name = "mbox-quic-" + suffix;
        byte[] auth = new byte[32];
        new SecureRandom().nextBytes(auth);
        StringBuilder encoded = new StringBuilder();
        for (byte b : auth) encoded.append(String.format(java.util.Locale.ROOT, "%02x", b & 0xff));
        token = encoded.toString();
        listener = new LocalServerSocket(name);
        worker = new Thread(this::loop, "MBox-quic-protect");
        worker.setDaemon(true);
        worker.start();
    }

    String socketAddress() { return "@" + name; }
    String token() { return token; }

    private void loop() {
        while (!closed) {
            try (LocalSocket incoming = listener.accept()) {
                incoming.setSoTimeout(5000);
                if (incoming.getPeerCredentials().getUid() != Process.myUid()) continue;
                byte[] proof = new byte[token.length()];
                new DataInputStream(incoming.getInputStream()).readFully(proof);
                FileDescriptor[] fds = incoming.getAncillaryFileDescriptors();
                byte status = 1;
                try {
                    if (fds != null && fds.length == 1 &&
                            java.security.MessageDigest.isEqual(proof, token.getBytes(StandardCharsets.US_ASCII))) {
                        // This is a pre-connect UDP socket. Bind only to a monitored physical Network.
                        physicalNetwork.bindSocket(fds[0]);
                        status = 0;
                        android.util.Log.i("MBox", "QUIC_DIAG stage=ANDROID_BIND code=OK");
                    }
                } catch (IOException failure) {
                    // Fail closed: the Go client will close its socket on NACK.
                    android.util.Log.w("MBox", "QUIC_DIAG stage=ANDROID_BIND code=FAIL reason="
                            + failure.getClass().getSimpleName() + ": " + failure.getMessage());
                } finally {
                    if (fds != null) for (FileDescriptor fd : fds) {
                        try { Os.close(fd); } catch (Exception ignored) { }
                    }
                }
                if (status != 0) android.util.Log.w("MBox", "QUIC_DIAG stage=ANDROID_BIND code=REJECT");
                incoming.getOutputStream().write(status);
            } catch (IOException failure) {
                if (!closed) android.util.Log.w("MBox", "QUIC socket protection unavailable");
            }
        }
    }

    @Override public void close() {
        closed = true;
        try { listener.close(); } catch (IOException ignored) { }
        worker.interrupt();
    }
}
