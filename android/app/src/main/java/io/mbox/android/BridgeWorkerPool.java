package io.mbox.android;

import java.io.Closeable;
import java.io.IOException;
import java.net.Socket;
import java.util.List;
import java.util.concurrent.ArrayBlockingQueue;
import java.util.concurrent.RejectedExecutionException;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.ThreadPoolExecutor;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;

/**
 * Bounded, app-private loopback work queue for Cronet bridge connections.
 *
 * Security invariant: a local process can open arbitrarily many TCP loopback
 * connections without knowing the bridge token. Each accepted connection must
 * occupy at most one of a fixed number of worker/queue slots; rejected and
 * not-yet-started connections must be closed, including during shutdown.
 */
final class BridgeWorkerPool implements Closeable {
    private final ThreadPoolExecutor workers;

    BridgeWorkerPool(int concurrency, int pending, ThreadFactory threadFactory) {
        if (concurrency <= 0 || pending <= 0) {
            throw new IllegalArgumentException("bridge worker limits must be positive");
        }
        workers = new ThreadPoolExecutor(concurrency, concurrency,
                0L, TimeUnit.MILLISECONDS, new ArrayBlockingQueue<>(pending),
                threadFactory, new ThreadPoolExecutor.AbortPolicy());
    }

    boolean dispatch(Socket socket, Consumer<Socket> handler) {
        SocketTask task = new SocketTask(socket, handler);
        try {
            workers.execute(task);
            return true;
        } catch (RejectedExecutionException overloaded) {
            // Do not let an unauthenticated loopback peer create an unbounded
            // backlog or kill the accept loop by overflowing the pool.
            task.closeSocket();
            return false;
        }
    }

    @Override
    public void close() {
        List<Runnable> neverStarted = workers.shutdownNow();
        // shutdownNow() does not run queued tasks, so their sockets would leak
        // without explicit cleanup.
        for (Runnable task : neverStarted) {
            if (task instanceof SocketTask) {
                ((SocketTask) task).closeSocket();
            }
        }
    }

    private static final class SocketTask implements Runnable {
        private final Socket socket;
        private final Consumer<Socket> handler;

        SocketTask(Socket socket, Consumer<Socket> handler) {
            this.socket = socket;
            this.handler = handler;
        }

        @Override
        public void run() {
            try {
                handler.accept(socket);
            } finally {
                closeSocket();
            }
        }

        void closeSocket() {
            try {
                socket.close();
            } catch (IOException ignored) {
            }
        }
    }
}
