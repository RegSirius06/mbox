package io.mbox.android;

import android.Manifest;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.net.Uri;
import android.net.VpnService;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.text.InputType;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.view.Gravity;
import android.view.View;
import android.widget.ScrollView;
import android.widget.TextView;

/** Small control surface for one imported MBox profile. */
public class MainActivity extends Activity {
    private static final int VPN_REQUEST = 1001;
    private static final int NOTIFICATION_REQUEST = 1002;

    private final Handler handler = new Handler(Looper.getMainLooper());
    private final Runnable refreshTask = new Runnable() {
        @Override
        public void run() {
            refreshUi();
            handler.postDelayed(this, 500);
        }
    };

    private EditText link;
    private TextView profile;
    private TextView status;
    private Button connectButton;
    private Button disconnectButton;
    private Button udpLabButton;
    private Button quicLabButton;
    private boolean compactUi;
    private String uiMessage = "";

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        buildUi();
        maybeRequestNotificationPermission();
        handleIntent(getIntent());
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        handleIntent(intent);
    }

    @Override
    protected void onResume() {
        super.onResume();
        handler.post(refreshTask);
    }

    @Override
    protected void onPause() {
        handler.removeCallbacks(refreshTask);
        super.onPause();
    }

    private void buildUi() {
        // Debug and release must have the same user-facing home screen.
        // Laboratory actions, if available, live behind Settings.
        compactUi = true;
        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(20), dp(24), dp(20), dp(24));

        TextView title = new TextView(this);
        title.setText("MBox");
        title.setTextSize(28);

        profile = new TextView(this);
        status = new TextView(this);

        connectButton = new Button(this);
        connectButton.setText("Connect");
        connectButton.setOnClickListener(v -> requestVpn());

        disconnectButton = new Button(this);
        disconnectButton.setText("Disconnect");
        disconnectButton.setOnClickListener(v -> {
            Intent intent = new Intent(this, MBoxVpnService.class)
                    .setAction(MBoxVpnService.ACTION_STOP);
            startService(intent);
        });

        Button clipboardButton = new Button(this);
        clipboardButton.setText("Import from clipboard");
        clipboardButton.setOnClickListener(v -> importFromClipboard());

        TextView manualLabel = new TextView(this);
        manualLabel.setText("Manual import");

        link = new EditText(this);
        link.setHint("mbox://…");
        link.setMinLines(2);
        link.setMaxLines(4);
        link.setHorizontallyScrolling(false);
        link.setVerticalScrollBarEnabled(true);
        link.setInputType(InputType.TYPE_CLASS_TEXT |
                InputType.TYPE_TEXT_FLAG_MULTI_LINE |
                InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);

        Button importButton = new Button(this);
        importButton.setText("Import pasted link");
        importButton.setOnClickListener(v -> importLink(link.getText().toString()));

        if (compactUi) {
            // Public UI: one connection action and one settings button.
            root.setGravity(Gravity.CENTER_HORIZONTAL);
            title.setTextSize(36);
            title.setPadding(0, dp(60), 0, dp(16));
            profile.setGravity(Gravity.CENTER);
            profile.setTextSize(14);
            status.setGravity(Gravity.CENTER);
            status.setTextSize(21);
            status.setPadding(0, dp(28), 0, dp(32));

            root.addView(title);
            root.addView(profile);
            root.addView(status);
            root.addView(connectButton, new LinearLayout.LayoutParams(-1, dp(56)));
            root.addView(disconnectButton, new LinearLayout.LayoutParams(-1, dp(56)));

            Button settings = new Button(this);
            settings.setText("Settings");
            settings.setOnClickListener(v -> showSettings());
            LinearLayout.LayoutParams settingsLayout = new LinearLayout.LayoutParams(-1, dp(48));
            settingsLayout.topMargin = dp(14);
            root.addView(settings, settingsLayout);
        } else {
            root.addView(title);
            root.addView(profile);
            root.addView(status);
            root.addView(connectButton);
            root.addView(disconnectButton);
            if (AppRuntime.udpLabEnabled(this)) {
                udpLabButton = new Button(this);
                udpLabButton.setText("UDP Lab: STUN through VPN");
                udpLabButton.setOnClickListener(v -> runUdpProbe());
                root.addView(udpLabButton);
                if (QuicLabTransport.configured(this)) {
                    quicLabButton = new Button(this);
                    quicLabButton.setOnClickListener(v -> {
                        if (MBoxVpnService.getState() != MBoxVpnService.State.STOPPED) {
                            uiMessage = "Disconnect the VPN before switching transport";
                        } else {
                            TransportPolicy.Mode selected = QuicLabTransport.cycleMode(this);
                            uiMessage = selected == TransportPolicy.Mode.AUTO
                                    ? "Auto: prefer QUIC, fall back to HTTPS via REG.RU"
                                    : selected == TransportPolicy.Mode.HTTPS
                                    ? "HTTPS only: REG.RU relay"
                                    : "QUIC only: direct VPS UDP";
                        }
                        refreshUi();
                    });
                    root.addView(quicLabButton);
                }
            }
            root.addView(clipboardButton);
            root.addView(manualLabel);
            root.addView(link);
            root.addView(importButton);
        }
        scroll.addView(root);
        setContentView(scroll);
        refreshUi();
    }

    private void showSettings() {
        final boolean hasQuic = QuicLabTransport.configured(this);
        final boolean debugLab = AppRuntime.udpLabEnabled(this);
        final boolean manualTransport = debugLab && hasQuic;
        java.util.ArrayList<String> options = new java.util.ArrayList<>();
        options.add("Import from clipboard");
        options.add("Paste profile link");
        if (manualTransport) options.add("Transport mode");
        if (debugLab) options.add("UDP Lab: STUN through VPN");
        new AlertDialog.Builder(this)
                .setTitle("MBox settings")
                .setItems(options.toArray(new String[0]), (dialog, choice) -> {
                    if (choice == 0) importFromClipboard();
                    else if (choice == 1) showImportDialog();
                    else if (choice == 2 && manualTransport) showTransportDialog();
                    else if (debugLab && choice == (manualTransport ? 3 : 2)) runUdpProbe();
                })
                .setNegativeButton("Close", null)
                .show();
    }

    private void showImportDialog() {
        EditText input = new EditText(this);
        input.setHint("mbox://…");
        input.setSingleLine(false);
        input.setMinLines(2);
        input.setMaxLines(5);
        input.setInputType(InputType.TYPE_CLASS_TEXT |
                InputType.TYPE_TEXT_FLAG_MULTI_LINE |
                InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        new AlertDialog.Builder(this)
                .setTitle("Import MBox profile")
                .setView(input)
                .setNegativeButton("Cancel", null)
                .setPositiveButton("Review", (d, w) -> importLink(input.getText().toString()))
                .show();
    }

    private void showTransportDialog() {
        if (!AppRuntime.udpLabEnabled(this)) return;
        if (MBoxVpnService.getState() != MBoxVpnService.State.STOPPED) {
            new AlertDialog.Builder(this)
                    .setMessage("Disconnect the VPN before changing transport mode.")
                    .setPositiveButton("OK", null)
                    .show();
            return;
        }
        TransportPolicy.Mode[] modes = {
                TransportPolicy.Mode.AUTO, TransportPolicy.Mode.HTTPS, TransportPolicy.Mode.QUIC};
        TransportPolicy.Mode current = QuicLabTransport.mode(this);
        int selected = current == modes[0] ? 0 : current == modes[1] ? 1 : 2;
        new AlertDialog.Builder(this)
                .setTitle("Transport mode")
                .setSingleChoiceItems(new String[]{"Auto (recommended)", "HTTPS", "QUIC"},
                        selected, (dialog, which) -> {
                            // Reuse the existing persisted selection API, no second preference.
                            for (int attempt = 0;
                                    attempt < 3 && QuicLabTransport.mode(this) != modes[which];
                                    attempt++) {
                                QuicLabTransport.cycleMode(this);
                            }
                            refreshUi();
                            dialog.dismiss();
                        })
                .setNegativeButton("Cancel", null)
                .show();
    }

    private void runUdpProbe() {
        if (!AppRuntime.udpLabEnabled(this) ||
                MBoxVpnService.getState() != MBoxVpnService.State.CONNECTED) {
            return;
        }
        if (udpLabButton != null) udpLabButton.setEnabled(false);
        new Thread(() -> {
            String result = UdpLabProbe.run(getApplicationContext());
            handler.post(() -> {
                if (udpLabButton != null) udpLabButton.setEnabled(true);
                new AlertDialog.Builder(this)
                        .setTitle("MBox UDP laboratory")
                        .setMessage(result + "\n\nA successful STUN response is not a leak audit.")
                        .setPositiveButton("OK", null)
                        .show();
            });
        }, "MBox-udp-probe").start();
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private void importFromClipboard() {
        ClipboardManager clipboard = (ClipboardManager) getSystemService(CLIPBOARD_SERVICE);
        if (clipboard == null || !clipboard.hasPrimaryClip()) {
            uiMessage = "Clipboard is empty";
            refreshUi();
            return;
        }

        ClipData clip = clipboard.getPrimaryClip();
        if (clip == null || clip.getItemCount() == 0) {
            uiMessage = "Clipboard is empty";
            refreshUi();
            return;
        }

        CharSequence text = clip.getItemAt(0).coerceToText(this);
        importLink(text == null ? "" : text.toString());
    }

    private void handleIntent(Intent intent) {
        if (intent == null || intent.getData() == null) {
            return;
        }
        Uri data = intent.getData();
        if ("mbox".equalsIgnoreCase(data.getScheme()) || "https".equalsIgnoreCase(data.getScheme())) {
            importLink(data.toString());
        }
    }

    private void importLink(String value) {
        try {
            ProfileStore.Candidate candidate = ProfileStore.parseLink(value);
            String message = "Profile: " + candidate.name + "\nClient: " + candidate.clientId + "\nEndpoint: " + candidate.endpoint + "\n\nServer key SHA-256:\n" + candidate.serverFingerprint + "\n\nVerify this fingerprint over a trusted channel before replacing the active profile." +
                    (candidate.quicUrl.isEmpty() ? "" : "\n\nQUIC: " + candidate.quicUrl + "\nQUIC certificate SHA-256: " + candidate.quicCertificateFingerprint);
            Runnable commit = () -> {
                try { String name = ProfileStore.commitCandidate(this, candidate); link.setText(""); uiMessage = "Imported: " + name; }
                catch (Exception error) { showImportError(error); }
                refreshUi();
            };
            AlertDialog.Builder confirm = new AlertDialog.Builder(this).setTitle("Confirm MBox profile").setMessage(message).setNegativeButton("Cancel", null);
            if (candidate.requiresDangerousConsent()) {
                confirm.setPositiveButton("Review unsafe options", (dialog, which) -> new AlertDialog.Builder(this)
                        .setTitle("Unsafe developer options")
                        .setMessage(candidate.securityWarningText() + "\n\nThese options can expose metadata or create a stable traffic fingerprint. Import only for an explicit diagnostic session.")
                        .setNegativeButton("Cancel", null)
                        .setPositiveButton("Import unsafe profile", (d, w) -> commit.run())
                        .show());
            } else {
                confirm.setPositiveButton("Replace profile", (dialog, which) -> commit.run());
            }
            confirm.show();
        } catch (Exception error) { showImportError(error); }
    }

    private void showImportError(Exception error) {
        // A persistent status message can be hidden by CONNECTING/FAILED states.
        // Show the failure without printing the secret provisioning link.
        String explanation = error.getMessage();
        if (explanation == null || explanation.trim().isEmpty()) explanation = "Unknown import error";
        explanation = MBoxLogSanitizer.sanitize(explanation);
        uiMessage = "Import error: " + explanation;
        refreshUi();
        new AlertDialog.Builder(this)
                .setTitle("Profile import failed")
                .setMessage(explanation)
                .setPositiveButton("OK", null)
                .show();
    }

    private void requestVpn() {
        if (!ProfileStore.hasProfile(this)) {
            uiMessage = "Import profile first";
            refreshUi();
            return;
        }

        Intent prepare = VpnService.prepare(this);
        if (prepare != null) {
            startActivityForResult(prepare, VPN_REQUEST);
        } else {
            startVpn();
        }
    }

    @Override
    protected void onActivityResult(int request, int result, Intent data) {
        super.onActivityResult(request, result, data);
        if (request == VPN_REQUEST && result == RESULT_OK) {
            startVpn();
        }
    }

    private void startVpn() {
        uiMessage = "";
        Intent intent = new Intent(this, MBoxVpnService.class)
                .setAction(MBoxVpnService.ACTION_START);
        startForegroundService(intent);
    }

    private void refreshUi() {
        boolean hasProfile = ProfileStore.hasProfile(this);
        profile.setText(hasProfile ? "Profile: " + ProfileStore.profileName(this) : "No profile");

        MBoxVpnService.State vpnState = MBoxVpnService.getState();
        switch (vpnState) {
            case DISCONNECTED:
                status.setText("Waiting for physical network…");
                break;
            case CONNECTING:
                status.setText("Connecting via " + MBoxVpnService.getActiveTransport() +
                        "\n" + MBoxVpnService.getConnectionPhase());
                break;
            case VERIFYING:
                status.setText("Verifying remote " + MBoxVpnService.getActiveTransport() +
                        " session…\n" + MBoxVpnService.getConnectionPhase());
                break;
            case RECOVERING:
                status.setText("Recovering " + MBoxVpnService.getActiveTransport() +
                        "\n" + MBoxVpnService.getConnectionPhase() +
                        (MBoxVpnService.getLastError().isEmpty() ? "" : "\nCause: " + MBoxVpnService.getLastError()));
                break;
            case CONNECTED:
                status.setText("Connected via " + MBoxVpnService.getActiveTransport() +
                        "\n" + MBoxVpnService.getConnectionPhase());
                break;
            case FAILED:
                status.setText("Error: " + MBoxVpnService.getLastError());
                break;
            case STOPPED:
            default:
                if (!uiMessage.isEmpty()) {
                    status.setText(uiMessage);
                } else if (!hasProfile) {
                    status.setText("Import an mbox:// profile");
                } else {
                    status.setText("Ready");
                }
                break;
        }

        if (quicLabButton != null) {
            TransportPolicy.Mode selected = QuicLabTransport.mode(this);
            quicLabButton.setText(selected == TransportPolicy.Mode.AUTO
                    ? "Transport: AUTO (QUIC → HTTPS via REG.RU) · tap for HTTPS"
                    : selected == TransportPolicy.Mode.HTTPS
                    ? "Transport: HTTPS via REG.RU · tap for QUIC"
                    : "Transport: QUIC direct to VPS · tap for AUTO");
            quicLabButton.setEnabled(vpnState == MBoxVpnService.State.STOPPED);
        }
        connectButton.setEnabled(hasProfile && vpnState != MBoxVpnService.State.CONNECTING &&
                vpnState != MBoxVpnService.State.VERIFYING &&
                vpnState != MBoxVpnService.State.RECOVERING &&
                vpnState != MBoxVpnService.State.CONNECTED);
        disconnectButton.setEnabled(vpnState == MBoxVpnService.State.DISCONNECTED ||
                vpnState == MBoxVpnService.State.CONNECTING ||
                vpnState == MBoxVpnService.State.VERIFYING ||
                vpnState == MBoxVpnService.State.RECOVERING ||
                vpnState == MBoxVpnService.State.CONNECTED ||
                vpnState == MBoxVpnService.State.FAILED);
        if (compactUi) {
            // Never display both connection actions together in the public UI.
            boolean running = vpnState == MBoxVpnService.State.DISCONNECTED ||
                    vpnState == MBoxVpnService.State.CONNECTING ||
                    vpnState == MBoxVpnService.State.VERIFYING ||
                    vpnState == MBoxVpnService.State.RECOVERING ||
                    vpnState == MBoxVpnService.State.CONNECTED ||
                    vpnState == MBoxVpnService.State.FAILED;
            connectButton.setVisibility(running ? View.GONE : View.VISIBLE);
            disconnectButton.setVisibility(running ? View.VISIBLE : View.GONE);
        }
    }

    private void maybeRequestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= 33 &&
                checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, NOTIFICATION_REQUEST);
        }
    }
}
