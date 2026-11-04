package io.mbox.android;

import java.net.Socket;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

/** Executable resource-invariant regression test; needs only the host JDK. */
public final class BridgeWorkerPoolHarness {
    private static void check(boolean condition, String reason) {
        if (!condition) throw new AssertionError(reason);
    }

    private static void closesOnOverloadAndShutdown() throws Exception {
        CountDownLatch entered = new CountDownLatch(2);
        CountDownLatch release = new CountDownLatch(1);
        BridgeWorkerPool pool = new BridgeWorkerPool(2, 3, r -> {
            Thread t = new Thread(r, "bridge-regression-worker");
            t.setDaemon(true);
            return t;
        });
        List<Socket> sockets = new ArrayList<>();
        AtomicInteger handled = new AtomicInteger();
        java.util.function.Consumer<Socket> blocking = s -> {
            handled.incrementAndGet();
            entered.countDown();
            try {
                release.await(5, TimeUnit.SECONDS);
            } catch (InterruptedException ignored) {
                Thread.currentThread().interrupt();
            }
        };
        try {
            for (int i = 0; i < 2; i++) {
                Socket s = new Socket();
                sockets.add(s);
                check(pool.dispatch(s, blocking), "worker dispatch refused");
            }
            check(entered.await(3, TimeUnit.SECONDS), "workers not started");
            for (int i = 0; i < 3; i++) {
                Socket s = new Socket();
                sockets.add(s);
                check(pool.dispatch(s, blocking), "queue slot refused");
            }
            for (int i = 0; i < 1000; i++) {
                Socket excess = new Socket();
                check(!pool.dispatch(excess, blocking), "unbounded queue accepted excess socket");
                check(excess.isClosed(), "overload left socket open");
            }
            check(handled.get() == 2, "overflow tasks were started");
            pool.close();
            for (int i = 2; i < sockets.size(); i++) {
                check(sockets.get(i).isClosed(), "shutdown leaked queued socket");
            }
            release.countDown();
            for (int attempt = 0; attempt < 100 && (!sockets.get(0).isClosed() || !sockets.get(1).isClosed()); attempt++) {
                Thread.sleep(10);
            }
            for (Socket s : sockets) check(s.isClosed(), "worker leaked socket");
            Socket afterClose = new Socket();
            check(!pool.dispatch(afterClose, blocking), "closed pool accepted connection");
            check(afterClose.isClosed(), "closed pool leaked socket");
        } finally {
            release.countDown();
            pool.close();
            for (Socket s : sockets) s.close();
        }
    }

    private static void closesAfterWorkerCompletion() throws Exception {
        try (BridgeWorkerPool pool = new BridgeWorkerPool(1, 1, r -> new Thread(r))) {
            Socket socket = new Socket();
            CountDownLatch entered = new CountDownLatch(1);
            check(pool.dispatch(socket, s -> entered.countDown()), "dispatch failed");
            check(entered.await(2, TimeUnit.SECONDS), "worker did not run");
            for (int i = 0; i < 100 && !socket.isClosed(); i++) Thread.sleep(10);
            check(socket.isClosed(), "completed worker leaked socket");
        }
    }

    public static void main(String[] args) throws Exception {
        closesOnOverloadAndShutdown();
        closesAfterWorkerCompletion();
        System.out.println("PASS: bounded Cronet loopback worker pool / socket lifecycle");
    }
}
