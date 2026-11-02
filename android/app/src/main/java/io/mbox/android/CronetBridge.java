package io.mbox.android;

import android.content.Context;
import android.net.Network;
import android.util.Log;

import org.chromium.net.CronetEngine;
import org.chromium.net.CronetException;
import org.chromium.net.NetworkException;
import org.chromium.net.RequestFinishedInfo;
import org.chromium.net.UrlRequest;
import org.chromium.net.UrlResponseInfo;
import org.chromium.net.apihelpers.UploadDataProviders;

import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.ByteArrayOutputStream;
import java.io.Closeable;
import java.io.DataInputStream;
import java.io.DataOutputStream;
import java.io.EOFException;
import java.io.IOException;
import java.net.InetAddress;
import java.net.ServerSocket;
import java.net.Socket;
import java.net.URI;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.util.ArrayList;
import java.util.Date;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * App-private loopback bridge from the Go core to Cronet.
 *
 * Go still owns MBox framing, Noise, replay and ACK state. This class owns only
 * the public HTTPS transaction so Android traffic uses a coherent Chromium
 * networking implementation instead of Go crypto/tls + net/http.
 */
final class CronetBridge implements Closeable {
    private static final String TAG = "MBox";
    private static final int REQUEST_MAGIC = 0x4d425231;  // MBR1
    private static final int RESPONSE_MAGIC = 0x4d425331; // MBS1
    private static final int MAX_BODY = 8 * 1024 * 1024;
    private static final int MAX_HEADERS = 64;
    private static final int MAX_HEADER_NAME = 128;
    private static final int MAX_HEADER_VALUE = 4096;
    private static final int REQUEST_TIMEOUT_SECONDS = 20;

    private final URI origin;
    private final boolean networkDebug;
    private final String token;
    private final ServerSocket server;
    private final CronetEngine engine;
    private final ExecutorService acceptExecutor;
    private final ExecutorService workerExecutor;
    private final ExecutorService callbackExecutor;
    private final ExecutorService metricsExecutor;
    private final java.util.Set<UrlRequest> activeRequests = ConcurrentHashMap.newKeySet();
    private final AtomicBoolean closed = new AtomicBoolean(false);

    private CronetBridge(Context context, String baseUrl, Network network, boolean debug) throws Exception {
        this.origin = validateOrigin(baseUrl);
        this.networkDebug = debug;
        this.token = randomToken();
        this.server = new ServerSocket(0, 32, InetAddress.getByName("127.0.0.1"));
        this.acceptExecutor = Executors.newSingleThreadExecutor(r -> namedThread(r, "MBox-cronet-accept"));
        this.workerExecutor = Executors.newFixedThreadPool(8, r -> namedThread(r, "MBox-cronet-worker"));
        this.callbackExecutor = Executors.newFixedThreadPool(4, r -> namedThread(r, "MBox-cronet-callback"));
        this.metricsExecutor = Executors.newSingleThreadExecutor(r -> namedThread(r, "MBox-cronet-metrics"));

        CronetEngine.Builder builder = new CronetEngine.Builder(context.getApplicationContext())
                .enableHttp2(true)
                .enableQuic(false)
                .enableHttpCache(CronetEngine.Builder.HTTP_CACHE_DISABLED, 0);
        this.engine = builder.build();
        if (network != null) {
            engine.bindToNetwork(network.getNetworkHandle());
        }

        acceptExecutor.execute(this::acceptLoop);
        Log.i(TAG, "NET stage=HTTP code=HTTP_STACK stack=cronet version=" + sanitize(engine.getVersionString()));
    }

    static CronetBridge start(Context context, String baseUrl, Network network, boolean debug) throws Exception {
        return new CronetBridge(context, baseUrl, network, debug);
    }

    String address() {
        return "127.0.0.1:" + server.getLocalPort();
    }

    String token() {
        return token;
    }

    private void acceptLoop() {
        while (!closed.get()) {
            try {
                Socket socket = server.accept();
                socket.setTcpNoDelay(true);
                workerExecutor.execute(() -> handle(socket));
            } catch (IOException error) {
                if (!closed.get()) {
                    Log.w(TAG, "Cronet bridge accept failed", error);
                }
                return;
            }
        }
    }

    private void handle(Socket socket) {
        try (Socket s = socket;
             DataInputStream in = new DataInputStream(new BufferedInputStream(s.getInputStream()));
             DataOutputStream out = new DataOutputStream(new BufferedOutputStream(s.getOutputStream()))) {
            s.setSoTimeout((REQUEST_TIMEOUT_SECONDS + 5) * 1000);
            BridgeRequest request = readRequest(in);
            if (!constantTimeEquals(token, request.token)) {
                writeResponse(out, BridgeResult.error("ERR_ANDROID_BRIDGE_AUTH", "invalid bridge token"));
                return;
            }
            BridgeResult result = execute(request);
            writeResponse(out, result);
        } catch (EOFException ignored) {
        } catch (Throwable error) {
            if (!closed.get()) {
                Log.w(TAG, "Cronet bridge request failed", error);
            }
        }
    }

    private BridgeRequest readRequest(DataInputStream in) throws Exception {
        if (in.readInt() != REQUEST_MAGIC) {
            throw new IOException("invalid bridge request magic");
        }
        String suppliedToken = readString16(in, 512);
        String method = readString16(in, 32);
        if (!"GET".equals(method) && !"POST".equals(method)) {
            throw new IOException("unsupported HTTP method");
        }
        String path = readString32(in, 64 * 1024);
        validatePath(path);
        int headerCount = in.readUnsignedShort();
        if (headerCount > MAX_HEADERS) {
            throw new IOException("too many request headers");
        }
        List<Header> headers = new ArrayList<>(headerCount);
        for (int i = 0; i < headerCount; i++) {
            String name = readString16(in, MAX_HEADER_NAME);
            String value = readString16(in, MAX_HEADER_VALUE);
            if (name.indexOf('\r') >= 0 || name.indexOf('\n') >= 0 || value.indexOf('\r') >= 0 || value.indexOf('\n') >= 0) {
                throw new IOException("invalid request header");
            }
            headers.add(new Header(name, value));
        }
        long bodyLength = Integer.toUnsignedLong(in.readInt());
        if (bodyLength > MAX_BODY) {
            throw new IOException("request body too large");
        }
        byte[] body = new byte[(int) bodyLength];
        in.readFully(body);
        return new BridgeRequest(suppliedToken, method, path, headers, body);
    }

    private BridgeResult execute(BridgeRequest request) throws Exception {
        String target = origin.resolve(request.path).toString();
        RequestResult result = new RequestResult();
        UrlRequest.Callback callback = new BridgeCallback(result);
        UrlRequest.Builder builder = engine.newUrlRequestBuilder(target, callback, callbackExecutor)
                .setHttpMethod(request.method)
                .disableCache()
                .setRequestFinishedListener(new RequestFinishedInfo.Listener(metricsExecutor) {
                    @Override
                    public void onRequestFinished(RequestFinishedInfo info) {
                        logFinished(info, request.path);
                    }
                });

        for (Header h : request.headers) {
            if (!isRestrictedHeader(h.name)) {
                builder.addHeader(h.name, h.value);
            }
        }
        if (request.body.length > 0) {
            builder.setUploadDataProvider(UploadDataProviders.create(request.body), callbackExecutor);
        }

        UrlRequest urlRequest = builder.build();
        result.request = urlRequest;
        activeRequests.add(urlRequest);
        try {
            urlRequest.start();
            if (!result.done.await(REQUEST_TIMEOUT_SECONDS, TimeUnit.SECONDS)) {
                urlRequest.cancel();
                return BridgeResult.error("ERR_HTTP_TIMEOUT", "Cronet request timed out");
            }
            return result.result;
        } finally {
            activeRequests.remove(urlRequest);
        }
    }

    private final class BridgeCallback extends UrlRequest.Callback {
        private final RequestResult state;
        private final ByteArrayOutputStream body = new ByteArrayOutputStream();
        private UrlResponseInfo responseInfo;

        BridgeCallback(RequestResult state) {
            this.state = state;
        }

        @Override
        public void onRedirectReceived(UrlRequest request, UrlResponseInfo info, String newLocationUrl) {
            complete(BridgeResult.response(info.getHttpStatusCode(), info.getNegotiatedProtocol(), new byte[0]));
            request.cancel();
        }

        @Override
        public void onResponseStarted(UrlRequest request, UrlResponseInfo info) {
            responseInfo = info;
            if (networkDebug) {
                Log.i(TAG, "NET stage=HTTP code=HTTP_HEADERS stack=cronet proto="
                        + sanitize(info.getNegotiatedProtocol()) + " status=" + info.getHttpStatusCode());
            }
            request.read(ByteBuffer.allocateDirect(32 * 1024));
        }

        @Override
        public void onReadCompleted(UrlRequest request, UrlResponseInfo info, ByteBuffer byteBuffer) {
            byteBuffer.flip();
            int n = byteBuffer.remaining();
            if (body.size() + n > MAX_BODY) {
                complete(BridgeResult.error("ERR_HTTP_BODY_TOO_LARGE", "response body exceeds bridge limit"));
                request.cancel();
                return;
            }
            byte[] chunk = new byte[n];
            byteBuffer.get(chunk);
            body.write(chunk, 0, chunk.length);
            byteBuffer.clear();
            request.read(byteBuffer);
        }

        @Override
        public void onSucceeded(UrlRequest request, UrlResponseInfo info) {
            complete(BridgeResult.response(info.getHttpStatusCode(), info.getNegotiatedProtocol(), body.toByteArray()));
        }

        @Override
        public void onFailed(UrlRequest request, UrlResponseInfo info, CronetException error) {
            complete(BridgeResult.error(classifyCronet(error), safeMessage(error)));
        }

        @Override
        public void onCanceled(UrlRequest request, UrlResponseInfo info) {
            if (!state.completed.get()) {
                complete(BridgeResult.error("ERR_HTTP_CANCELED", "Cronet request canceled"));
            }
        }

        private void complete(BridgeResult result) {
            if (state.completed.compareAndSet(false, true)) {
                state.result = result;
                state.done.countDown();
            }
        }
    }

    private void logFinished(RequestFinishedInfo info, String path) {
        if (!networkDebug) {
            return;
        }
        RequestFinishedInfo.Metrics m = info.getMetrics();
        UrlResponseInfo response = info.getResponseInfo();
        String proto = response == null ? "none" : sanitize(response.getNegotiatedProtocol());
        String code = classifyFinished(info);
        Log.i(TAG, "NET stage=CRONET code=" + code
                + " proto=" + proto
                + " reused=" + m.getSocketReused()
                + " dns_ms=" + duration(m.getDnsStart(), m.getDnsEnd())
                + " connect_ms=" + duration(m.getConnectStart(), m.getConnectEnd())
                + " tls_ms=" + duration(m.getSslStart(), m.getSslEnd())
                + " ttfb_ms=" + nullableLong(m.getTtfbMs())
                + " total_ms=" + nullableLong(m.getTotalTimeMs())
                + " path_hash=" + shortHash(path));
    }

    private static String classifyFinished(RequestFinishedInfo info) {
        if (info.getFinishedReason() == RequestFinishedInfo.SUCCEEDED) {
            return "CRONET_OK";
        }
        CronetException error = info.getException();
        if (error instanceof NetworkException) {
            return classifyCronet(error);
        }
        RequestFinishedInfo.Metrics m = info.getMetrics();
        if (m.getSslStart() != null && m.getSslEnd() == null) {
            return "ERR_TLS_HANDSHAKE";
        }
        if (m.getConnectStart() != null && m.getConnectEnd() == null) {
            return "ERR_TCP_CONNECT";
        }
        if (m.getDnsStart() != null && m.getDnsEnd() == null) {
            return "ERR_DNS";
        }
        return "ERR_CRONET";
    }

    private static String classifyCronet(CronetException error) {
        if (!(error instanceof NetworkException)) {
            return "ERR_CRONET";
        }
        switch (((NetworkException) error).getErrorCode()) {
            case NetworkException.ERROR_HOSTNAME_NOT_RESOLVED:
                return "ERR_DNS";
            case NetworkException.ERROR_INTERNET_DISCONNECTED:
                return "ERR_NETWORK_DISCONNECTED";
            case NetworkException.ERROR_NETWORK_CHANGED:
                return "ERR_NETWORK_CHANGED";
            case NetworkException.ERROR_TIMED_OUT:
            case NetworkException.ERROR_CONNECTION_TIMED_OUT:
                return "ERR_TCP_TIMEOUT";
            case NetworkException.ERROR_CONNECTION_REFUSED:
                return "ERR_TCP_REFUSED";
            case NetworkException.ERROR_CONNECTION_RESET:
                return "ERR_TCP_RESET";
            case NetworkException.ERROR_ADDRESS_UNREACHABLE:
                return "ERR_TCP_UNREACHABLE";
            case NetworkException.ERROR_QUIC_PROTOCOL_FAILED:
                return "ERR_QUIC";
            default:
                return "ERR_CRONET_OTHER";
        }
    }

    private static boolean isRestrictedHeader(String name) {
        String n = name.toLowerCase(Locale.ROOT);
        return n.equals("host") || n.equals("connection") || n.equals("proxy-connection")
                || n.equals("content-length") || n.equals("transfer-encoding")
                || n.equals("upgrade") || n.equals("user-agent") || n.equals("accept-encoding");
    }

    private static void validatePath(String path) throws IOException {
        if (path == null || path.isEmpty() || path.charAt(0) != '/' || path.startsWith("//")
                || path.indexOf('\r') >= 0 || path.indexOf('\n') >= 0) {
            throw new IOException("invalid bridge path");
        }
        URI parsed = URI.create(path);
        if (parsed.isAbsolute() || parsed.getRawAuthority() != null) {
            throw new IOException("bridge path must be relative to configured origin");
        }
    }

    private static URI validateOrigin(String raw) throws Exception {
        URI u = new URI(raw);
        if (!"https".equalsIgnoreCase(u.getScheme()) || u.getHost() == null || u.getRawUserInfo() != null
                || u.getRawQuery() != null || u.getRawFragment() != null || raw.endsWith("/")) {
            throw new Exception("Cronet bridge requires a clean HTTPS transport URL");
        }
        return u;
    }

    private static String readString16(DataInputStream in, int max) throws IOException {
        int n = in.readUnsignedShort();
        if (n > max) {
            throw new IOException("bridge string exceeds limit");
        }
        byte[] data = new byte[n];
        in.readFully(data);
        return new String(data, StandardCharsets.UTF_8);
    }

    private static String readString32(DataInputStream in, int max) throws IOException {
        long n = Integer.toUnsignedLong(in.readInt());
        if (n > max) {
            throw new IOException("bridge string exceeds limit");
        }
        byte[] data = new byte[(int) n];
        in.readFully(data);
        return new String(data, StandardCharsets.UTF_8);
    }

    private static void writeResponse(DataOutputStream out, BridgeResult result) throws IOException {
        out.writeInt(RESPONSE_MAGIC);
        out.writeShort(result.status & 0xffff);
        writeString16(out, result.errorCode, 128);
        writeString16(out, result.errorMessage, 1024);
        writeString16(out, result.protocol, 128);
        out.writeInt(result.body.length);
        out.write(result.body);
        out.flush();
    }

    private static void writeString16(DataOutputStream out, String value, int max) throws IOException {
        byte[] data = value == null ? new byte[0] : value.getBytes(StandardCharsets.UTF_8);
        if (data.length > max || data.length > 0xffff) {
            throw new IOException("bridge response string exceeds limit");
        }
        out.writeShort(data.length);
        out.write(data);
    }

    private static String randomToken() throws Exception {
        byte[] bytes = new byte[32];
        new SecureRandom().nextBytes(bytes);
        StringBuilder out = new StringBuilder(64);
        for (byte b : bytes) {
            out.append(String.format(Locale.ROOT, "%02x", b & 0xff));
        }
        return out.toString();
    }

    private static boolean constantTimeEquals(String a, String b) {
        byte[] aa = a.getBytes(StandardCharsets.UTF_8);
        byte[] bb = b.getBytes(StandardCharsets.UTF_8);
        int diff = aa.length ^ bb.length;
        int n = Math.max(aa.length, bb.length);
        for (int i = 0; i < n; i++) {
            byte av = i < aa.length ? aa[i] : 0;
            byte bv = i < bb.length ? bb[i] : 0;
            diff |= av ^ bv;
        }
        return diff == 0;
    }

    private static Thread namedThread(Runnable r, String name) {
        Thread t = new Thread(r, name);
        t.setDaemon(true);
        return t;
    }

    private static long duration(Date start, Date end) {
        return start == null || end == null ? -1 : Math.max(0, end.getTime() - start.getTime());
    }

    private static String nullableLong(Long value) {
        return value == null ? "-1" : Long.toString(value);
    }

    private static String shortHash(String value) {
        int h = value == null ? 0 : value.hashCode();
        return String.format(Locale.ROOT, "%08x", h);
    }

    private static String sanitize(String value) {
        if (value == null || value.isEmpty()) {
            return "none";
        }
        value = value.replace('\r', '_').replace('\n', '_');
        return value.length() > 120 ? value.substring(0, 120) : value;
    }

    private static String safeMessage(Throwable error) {
        String message = error == null ? "" : error.getMessage();
        if (message == null || message.isEmpty()) {
            message = error == null ? "unknown" : error.getClass().getSimpleName();
        }
        message = message.replace('\r', '_').replace('\n', '_');
        return message.length() > 180 ? message.substring(0, 180) : message;
    }

    @Override
    public void close() {
        if (!closed.compareAndSet(false, true)) {
            return;
        }
        try {
            server.close();
        } catch (IOException ignored) {
        }
        for (UrlRequest request : activeRequests) {
            try {
                request.cancel();
            } catch (Throwable ignored) {
            }
        }
        activeRequests.clear();
        acceptExecutor.shutdownNow();
        workerExecutor.shutdownNow();
        callbackExecutor.shutdownNow();
        metricsExecutor.shutdownNow();
        try {
            engine.shutdown();
        } catch (Throwable error) {
            Log.w(TAG, "Cronet shutdown failed", error);
        }
    }

    private static final class Header {
        final String name;
        final String value;
        Header(String name, String value) {
            this.name = name;
            this.value = value;
        }
    }

    private static final class BridgeRequest {
        final String token;
        final String method;
        final String path;
        final List<Header> headers;
        final byte[] body;
        BridgeRequest(String token, String method, String path, List<Header> headers, byte[] body) {
            this.token = token;
            this.method = method;
            this.path = path;
            this.headers = headers;
            this.body = body;
        }
    }

    private static final class BridgeResult {
        final int status;
        final String protocol;
        final byte[] body;
        final String errorCode;
        final String errorMessage;

        private BridgeResult(int status, String protocol, byte[] body, String errorCode, String errorMessage) {
            this.status = status;
            this.protocol = protocol == null ? "" : protocol;
            this.body = body == null ? new byte[0] : body;
            this.errorCode = errorCode == null ? "" : errorCode;
            this.errorMessage = errorMessage == null ? "" : errorMessage;
        }

        static BridgeResult response(int status, String protocol, byte[] body) {
            return new BridgeResult(status, protocol, body, "", "");
        }

        static BridgeResult error(String code, String message) {
            return new BridgeResult(0, "", new byte[0], code, message);
        }
    }

    private static final class RequestResult {
        final CountDownLatch done = new CountDownLatch(1);
        final AtomicBoolean completed = new AtomicBoolean(false);
        volatile UrlRequest request;
        volatile BridgeResult result = BridgeResult.error("ERR_CRONET", "request did not complete");
    }
}
