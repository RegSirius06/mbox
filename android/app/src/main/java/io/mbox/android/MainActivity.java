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

        root.addView(title);
        root.addView(profile);
        root.addView(status);
        root.addView(connectButton);
        root.addView(disconnectButton);
        root.addView(clipboardButton);
        root.addView(manualLabel);
        root.addView(link);
        root.addView(importButton);

        scroll.addView(root);
        setContentView(scroll);
        refreshUi();
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
            String message = "Profile: " + candidate.name + "\nClient: " + candidate.clientId + "\nEndpoint: " + candidate.endpoint + "\n\nServer key SHA-256:\n" + candidate.serverFingerprint + "\n\nVerify this fingerprint over a trusted channel before replacing the active profile.";
            Runnable commit = () -> {
                try { String name = ProfileStore.commitCandidate(this, candidate); link.setText(""); uiMessage = "Imported: " + name; }
                catch (Exception error) { uiMessage = "Import error: " + error.getMessage(); }
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
        } catch (Exception error) { uiMessage = "Import error: " + error.getMessage(); refreshUi(); }
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
                status.setText("Connecting…");
                break;
            case RECOVERING:
                status.setText("Reconnecting…" + (MBoxVpnService.getLastError().isEmpty() ? "" : "\n" + MBoxVpnService.getLastError()));
                break;
            case CONNECTED:
                status.setText("Connected");
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

        connectButton.setEnabled(hasProfile && vpnState != MBoxVpnService.State.CONNECTING &&
                vpnState != MBoxVpnService.State.RECOVERING &&
                vpnState != MBoxVpnService.State.CONNECTED);
        disconnectButton.setEnabled(vpnState == MBoxVpnService.State.DISCONNECTED ||
                vpnState == MBoxVpnService.State.CONNECTING ||
                vpnState == MBoxVpnService.State.RECOVERING ||
                vpnState == MBoxVpnService.State.CONNECTED ||
                vpnState == MBoxVpnService.State.FAILED);
    }

    private void maybeRequestNotificationPermission() {
        if (Build.VERSION.SDK_INT >= 33 &&
                checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED) {
            requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, NOTIFICATION_REQUEST);
        }
    }
}
