package io.mbox.android;

import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import android.net.NetworkRequest;
import android.os.Handler;
import android.os.Looper;

/**
 * Tracks the preferred physical Internet network beneath MBox.
 *
 * This deliberately ignores VPN networks. A default-network callback is not
 * suitable here because establishing our own VpnService can itself change the
 * process-visible default network and create a self-triggered reconnect loop.
 */
final class UnderlyingNetworkMonitor implements AutoCloseable {
    private static final long DEBOUNCE_MS = 250L;
    interface Listener {
        void onUnderlyingNetworkChanged(Network previous, Network current);
    }

    private final ConnectivityManager connectivityManager;
    private final Listener listener;
    private final ConnectivityManager.NetworkCallback callback;
    private final Handler handler;
    private final Runnable reconcileTask;
    private volatile Network current;
    private volatile boolean closed;

    UnderlyingNetworkMonitor(ConnectivityManager connectivityManager, Listener listener) {
        this.connectivityManager = connectivityManager;
        this.listener = listener;
        this.handler = new Handler(Looper.getMainLooper());
        this.reconcileTask = this::reconcile;
        this.current = selectPreferredNetwork();
        this.callback = new ConnectivityManager.NetworkCallback() {
            @Override
            public void onAvailable(Network network) {
                scheduleReconcile();
            }

            @Override
            public void onLost(Network network) {
                scheduleReconcile();
            }

            @Override
            public void onCapabilitiesChanged(Network network, NetworkCapabilities capabilities) {
                scheduleReconcile();
            }
        };

        NetworkRequest request = new NetworkRequest.Builder()
                .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
                .build();
        connectivityManager.registerNetworkCallback(request, callback);
    }

    synchronized Network currentNetwork() {
        Network selected = selectPreferredNetwork();
        current = selected;
        if (selected != null) {
            notifyAll();
        }
        return selected;
    }

    synchronized Network awaitUsableNetwork() throws InterruptedException {
        while (!closed) {
            Network selected = selectPreferredNetwork();
            current = selected;
            if (selected != null) {
                return selected;
            }
            wait();
        }
        return null;
    }

    private void scheduleReconcile() {
        if (closed) {
            return;
        }
        handler.removeCallbacks(reconcileTask);
        handler.postDelayed(reconcileTask, DEBOUNCE_MS);
    }

    private void reconcile() {
        Network previous;
        Network selected;
        boolean notifyListener;
        synchronized (this) {
            if (closed) {
                return;
            }
            selected = selectPreferredNetwork();
            previous = current;
            if (sameNetwork(previous, selected)) {
                if (selected != null) {
                    notifyAll();
                }
                return;
            }
            current = selected;
            notifyAll();

            // Initial discovery is baseline state, not a handover.
            notifyListener = !(previous == null && selected != null);
        }
        if (notifyListener) {
            listener.onUnderlyingNetworkChanged(previous, selected);
        }
    }

    private Network selectPreferredNetwork() {
        Network active = connectivityManager.getActiveNetwork();
        if (isUsablePhysicalNetwork(active)) {
            return active;
        }

        Network best = null;
        int bestScore = Integer.MIN_VALUE;
        for (Network candidate : connectivityManager.getAllNetworks()) {
            NetworkCapabilities capabilities = connectivityManager.getNetworkCapabilities(candidate);
            if (!isUsablePhysicalCapabilities(capabilities)) {
                continue;
            }
            int score = score(capabilities);
            if (candidate.equals(current)) {
                score += 5; // avoid churn when two candidates are otherwise equivalent
            }
            if (score > bestScore) {
                best = candidate;
                bestScore = score;
            }
        }
        return best;
    }

    private boolean isUsablePhysicalNetwork(Network network) {
        if (network == null) {
            return false;
        }
        return isUsablePhysicalCapabilities(connectivityManager.getNetworkCapabilities(network));
    }

    private static boolean isUsablePhysicalCapabilities(NetworkCapabilities capabilities) {
        return capabilities != null
                && capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                && capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
                && !capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN);
    }

    private static int score(NetworkCapabilities capabilities) {
        int score = 0;
        if (capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)) {
            score += 1000;
        }
        if (capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)) {
            score += 100;
        }
        if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) {
            score += 40;
        } else if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)) {
            score += 30;
        } else if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)) {
            score += 20;
        }
        return score;
    }

    private static boolean sameNetwork(Network a, Network b) {
        return a == b || (a != null && a.equals(b));
    }

    @Override
    public synchronized void close() {
        if (closed) {
            return;
        }
        closed = true;
        handler.removeCallbacks(reconcileTask);
        notifyAll();
        try {
            connectivityManager.unregisterNetworkCallback(callback);
        } catch (RuntimeException ignored) {
            // Callback may already have been unregistered during service teardown.
        }
    }
}
