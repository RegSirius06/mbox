package io.mbox.android;
// MBOX_BETA43_RELEASE_CONSISTENCY9
// MBOX_BETA43_THEME_BRAND8

import android.Manifest;
import android.app.Activity;
import android.app.AlertDialog;
import android.content.ClipData;
import android.content.ClipboardManager;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.drawable.GradientDrawable;
import android.graphics.Typeface;
import android.graphics.Color;
import android.net.Uri;
import android.net.VpnService;
import android.os.Build;
import android.provider.Settings;
import android.app.NotificationManager;
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
    private TextView subscriptionStatus;
    private boolean subscriptionConnecting;
    private Button connectButton;
    private Button disconnectButton;
    private Button udpLabButton;
    private Button quicLabButton;
    private boolean compactUi;
    // MBOX_BETA43_POLISH: hidden read-only diagnostics, unlocked by seven logo taps.
    private int diagnosticTaps;

    // MBOX_BETA43_NATIVE_SCREENS: screens are real Activity content, not dialog windows.
    private enum Page { HOME, SETTINGS, PROFILES, SUBSCRIPTIONS, LANGUAGE, DELETE_PROFILE, LAB, DIAGNOSTICS }
    private Page currentPage = Page.HOME;
    private TextView diagnosticsView;
    private long lastDiagnosticsRedraw;
    private long firstEasterTapAt;

    private String uiMessage = "";

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        buildUi();
        maybeRequestNotificationPermission();
        if (!handleReleaseIntent(getIntent())) handleIntent(getIntent());
    }

    @Override
    protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        if (intent != null && (ReleaseUpdates.ACTION_STAY.equals(intent.getAction())
                || ReleaseUpdates.ACTION_GITHUB.equals(intent.getAction()))) {
            buildUi();
            handleReleaseIntent(intent);
            return;
        }
        if (intent != null && MBoxVpnService.ACTION_OPEN_STATUS.equals(intent.getAction())) {
            buildUi();
            return;
        }
        handleIntent(intent);
    }

    @Override
    protected void onResume() {
        super.onResume();
        handler.post(refreshTask);
        // Refresh only while the VPN is stopped. Never force a subscription
        // network request through an active or transitioning VPN tunnel.
        if (SubscriptionStore.isConfigured(this) &&
                MBoxVpnService.getState() == MBoxVpnService.State.STOPPED) {
            refreshSubscription(false);
        }
    }

    @Override
    protected void onPause() {
        handler.removeCallbacks(refreshTask);
        super.onPause();
    }

    // MBOX_BETA43_UX_LANGUAGE: preference is app-local, without VPN restart.
    private String ui(String english) { return UiLanguage.text(this, english); }

    private void buildUi() {
        currentPage = Page.HOME;
        diagnosticsView = null;
        // Debug and release must have the same user-facing home screen.
        // Laboratory actions, if available, live behind Settings.
        compactUi = true;
        ScrollView scroll = new ScrollView(this);
        scroll.setBackgroundColor(getColor(R.color.mbox_bg));
        scroll.setFillViewport(true);

        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setPadding(dp(20), dp(24), dp(20), dp(24));

        TextView title = new TextView(this);
        title.setText(ui("MBox"));
        // Easter egg: seven taps on the logo within four seconds, no visible
        // diagnostics entry or accessibility hint revealing the trigger.
        title.setOnClickListener(v -> {
            long now = android.os.SystemClock.elapsedRealtime();
            if (diagnosticTaps == 0 || now - firstEasterTapAt > 4000) {
                diagnosticTaps = 0;
                firstEasterTapAt = now;
            }
            if (++diagnosticTaps >= 7) {
                diagnosticTaps = 0;
                showDiagnostics();
            }
        });
        title.setTextSize(28);

        profile = new TextView(this);
        profile.setContentDescription("Open profiles and subscriptions");
        profile.setOnClickListener(v -> showProfileHub());
        status = new TextView(this);
        subscriptionStatus = new TextView(this);
        subscriptionStatus.setTextSize(12);
        subscriptionStatus.setGravity(Gravity.CENTER);

        connectButton = new Button(this);
        connectButton.setText(ui("Connect"));
        connectButton.setOnClickListener(v -> requestVpn());

        disconnectButton = new Button(this);
        disconnectButton.setText(ui("Disconnect"));
        disconnectButton.setOnClickListener(v -> {
            Intent intent = new Intent(this, MBoxVpnService.class)
                    .setAction(MBoxVpnService.ACTION_STOP);
            startService(intent);
        });

        Button clipboardButton = new Button(this);
        clipboardButton.setText(ui("Import from clipboard"));
        clipboardButton.setOnClickListener(v -> importFromClipboard());

        TextView manualLabel = new TextView(this);
        manualLabel.setText(ui("Manual import"));

        link = new EditText(this);
        link.setHint(ui("mbox://…"));
        link.setMinLines(2);
        link.setMaxLines(4);
        link.setHorizontallyScrolling(false);
        link.setVerticalScrollBarEnabled(true);
        link.setInputType(InputType.TYPE_CLASS_TEXT |
                InputType.TYPE_TEXT_FLAG_MULTI_LINE |
                InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);

        Button importButton = new Button(this);
        importButton.setText(ui("Import pasted link"));
        importButton.setOnClickListener(v -> importLink(link.getText().toString()));

        if (compactUi) {
            // MBOX_COMPACT_HOME_V2: one primary action, one settings action.
            root.setGravity(Gravity.CENTER_HORIZONTAL);
            root.setBackgroundColor(getColor(R.color.mbox_bg));
            root.setPadding(dp(24), dp(48), dp(24), dp(32));

            TextView eyebrow = new TextView(this);
            eyebrow.setText(ui("PRIVATE CONNECTION"));
            eyebrow.setTextSize(11);
            eyebrow.setLetterSpacing(0.16f);
            eyebrow.setTextColor(getColor(R.color.mbox_accent));
            eyebrow.setGravity(Gravity.CENTER);
            root.addView(eyebrow);

            title.setTextSize(42);
            title.setTypeface(null, Typeface.BOLD);
            title.setTextColor(getColor(R.color.mbox_text));
            title.setGravity(Gravity.CENTER);
            title.setPadding(0, dp(10), 0, dp(8));
            root.addView(title);

            TextView lead = new TextView(this);
            lead.setText(ui("Your connection, under your control."));
            lead.setTextSize(14);
            lead.setGravity(Gravity.CENTER);
            lead.setTextColor(Color.rgb(165, 180, 199));
            root.addView(lead);

            LinearLayout panel = new LinearLayout(this);
            panel.setOrientation(LinearLayout.VERTICAL);
            panel.setGravity(Gravity.CENTER_HORIZONTAL);
            panel.setPadding(dp(20), dp(25), dp(20), dp(25));
            GradientDrawable panelBackground = new GradientDrawable();
            panelBackground.setColor(getColor(R.color.mbox_surface));
            panelBackground.setCornerRadius(dp(24));
            panelBackground.setStroke(dp(1), getColor(R.color.mbox_border));
            panel.setBackground(panelBackground);

            profile.setGravity(Gravity.CENTER);
            profile.setTextSize(14);
            profile.setTextColor(Color.rgb(200, 213, 230));
            subscriptionStatus.setTextColor(Color.rgb(139, 161, 182));
            status.setGravity(Gravity.CENTER);
            status.setTextSize(19);
            status.setTypeface(null, Typeface.BOLD);
            status.setPadding(0, dp(22), 0, dp(8));
            panel.addView(profile);
            panel.addView(subscriptionStatus);
            panel.addView(status);
            LinearLayout.LayoutParams panelParams = new LinearLayout.LayoutParams(-1, -2);
            panelParams.topMargin = dp(42);
            panelParams.bottomMargin = dp(26);
            root.addView(panel, panelParams);

            GradientDrawable primaryBackground = new GradientDrawable();
            primaryBackground.setColor(getColor(R.color.mbox_primary_bg));
            primaryBackground.setCornerRadius(dp(18));
            connectButton.setBackground(primaryBackground);
            // Custom GradientDrawable must not inherit the platform Button tint.
            connectButton.setBackgroundTintList(null);
            connectButton.setTextColor(getColor(R.color.mbox_on_primary_bg));
            connectButton.setAllCaps(false);
            connectButton.setTextSize(17);
            connectButton.setTypeface(null, Typeface.BOLD);

            GradientDrawable stopBackground = new GradientDrawable();
            stopBackground.setColor(getColor(R.color.mbox_stop_bg));
            stopBackground.setCornerRadius(dp(18));
            disconnectButton.setBackground(stopBackground);
            disconnectButton.setBackgroundTintList(null);
            disconnectButton.setTextColor(getColor(R.color.mbox_on_stop_bg));
            disconnectButton.setAllCaps(false);
            disconnectButton.setTextSize(17);
            root.addView(connectButton, new LinearLayout.LayoutParams(-1, dp(60)));
            root.addView(disconnectButton, new LinearLayout.LayoutParams(-1, dp(60)));

            Button settings = new Button(this);
            settings.setText(ui("Profiles & settings"));
            settings.setAllCaps(false);
            settings.setTextSize(15);
            settings.setTextColor(Color.rgb(202, 216, 234));
            GradientDrawable secondaryBackground = new GradientDrawable();
            secondaryBackground.setColor(getColor(R.color.mbox_surface));
            secondaryBackground.setCornerRadius(dp(16));
            secondaryBackground.setStroke(dp(1), getColor(R.color.mbox_border));
            settings.setBackground(secondaryBackground);
            settings.setOnClickListener(v -> showSettingsHome());
            LinearLayout.LayoutParams settingsLayout = new LinearLayout.LayoutParams(-1, dp(54));
            settingsLayout.topMargin = dp(14);
            root.addView(settings, settingsLayout);

            View footerSpace = new View(this);
            root.addView(footerSpace, new LinearLayout.LayoutParams(1, 0, 1f));
            TextView releases = new TextView(this);
            releases.setText(ui("GitHub · Releases"));
            releases.setTextSize(13);
            releases.setTextColor(getColor(R.color.mbox_muted));
            releases.setGravity(Gravity.CENTER);
            releases.setPadding(dp(4), dp(8), dp(4), dp(8));
            releases.setOnClickListener(v -> openGithubReleases());
            root.addView(releases, new LinearLayout.LayoutParams(-1, dp(48)));
        } else {
            root.addView(title);
            root.addView(profile);
            root.addView(subscriptionStatus);
            root.addView(status);
            root.addView(connectButton);
            root.addView(disconnectButton);
            if (AppRuntime.udpLabEnabled(this)) {
                udpLabButton = new Button(this);
                udpLabButton.setText(ui("UDP Lab: STUN through VPN"));
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



    // MBOX_BETA43_NATIVE_SCREENS: no second Activity and no new privileged service.
    private LinearLayout openPage(String heading, String subtitle, Page page) {
        currentPage = page;
        diagnosticsView = null;
        ScrollView scroll = new ScrollView(this);
        scroll.setFillViewport(true);
        scroll.setBackgroundColor(getColor(R.color.mbox_bg));
        LinearLayout content = new LinearLayout(this);
        content.setOrientation(LinearLayout.VERTICAL);
        content.setPadding(dp(22), dp(18), dp(22), dp(30));

        TextView back = new TextView(this);
        back.setText(ui("‹  Back"));
        back.setContentDescription("Back to MBox");
        back.setTextSize(16);
        back.setTextColor(getColor(R.color.mbox_accent));
        back.setPadding(0, dp(8), 0, dp(24));
        back.setOnClickListener(v -> navigateBack());
        content.addView(back);

        TextView titleView = new TextView(this);
        titleView.setText(ui(heading));
        titleView.setTextSize(30);
        titleView.setTypeface(null, Typeface.BOLD);
        titleView.setTextColor(getColor(R.color.mbox_text));
        content.addView(titleView);

        TextView lead = new TextView(this);
        lead.setText(ui(subtitle));
        lead.setTextSize(14);
        lead.setTextColor(getColor(R.color.mbox_muted));
        lead.setPadding(0, dp(10), 0, dp(26));
        content.addView(lead);
        scroll.addView(content);
        setContentView(scroll);
        return content;
    }

    private void addPageAction(LinearLayout content, String title, String subtitle,
                               Runnable action) {
        LinearLayout card = new LinearLayout(this);
        card.setOrientation(LinearLayout.VERTICAL);
        card.setPadding(dp(18), dp(16), dp(18), dp(16));
        GradientDrawable background = new GradientDrawable();
        background.setColor(getColor(R.color.mbox_surface));
        background.setCornerRadius(dp(17));
        background.setStroke(dp(1), getColor(R.color.mbox_border));
        card.setBackground(background);
        card.setClickable(true);
        card.setFocusable(true);
        card.setOnClickListener(v -> action.run());
        TextView name = new TextView(this);
        name.setText(ui(title.replace("  ✓", "")) + (title.endsWith("  ✓") ? "  ✓" : "") + "  ›");
        name.setTypeface(null, Typeface.BOLD);
        name.setTextColor(getColor(R.color.mbox_text));
        name.setTextSize(16);
        card.addView(name);
        if (!subtitle.isEmpty()) {
            TextView description = new TextView(this);
            description.setText(ui(subtitle));
            description.setTextSize(13);
            description.setTextColor(getColor(R.color.mbox_muted));
            description.setPadding(0, dp(7), 0, 0);
            card.addView(description);
        }
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(-1, -2);
        lp.bottomMargin = dp(12);
        content.addView(card, lp);
    }

    private void navigateBack() {
        if (currentPage == Page.HOME) {
            super.onBackPressed();
        } else if (currentPage == Page.DELETE_PROFILE || currentPage == Page.SUBSCRIPTIONS) {
            showProfileHub();
        } else if (currentPage == Page.LAB) {
            showDiagnostics();
        } else if (currentPage == Page.LANGUAGE || currentPage == Page.PROFILES) {
            showSettingsHome();
        } else {
            buildUi();
        }
    }

    @Override
    @SuppressWarnings("deprecation")
    public void onBackPressed() {
        navigateBack();
    }

    private boolean handleReleaseIntent(Intent intent) {
        if (intent == null) return false;
        String action = intent.getAction();
        if (!ReleaseUpdates.ACTION_STAY.equals(action)
                && !ReleaseUpdates.ACTION_GITHUB.equals(action)) return false;
        ReleaseUpdates.dismiss(this);
        if (ReleaseUpdates.ACTION_GITHUB.equals(action)) openGithubReleases();
        return true;
    }

    private void openGithubReleases() {
        try {
            startActivity(new Intent(Intent.ACTION_VIEW,
                    Uri.parse(ReleaseUpdates.releasesUrl())));
        } catch (android.content.ActivityNotFoundException error) {
            uiMessage = ui("No browser available");
            refreshUi();
        }
    }

    private void showSettingsHome() {
        LinearLayout content = openPage("Settings", "Everything you need, in one place.",
                Page.SETTINGS);
        // MBOX_BETA43_RELEASE_UPDATES_FINAL: clipboard is two taps from home.
        addPageAction(content, "Import from clipboard", "Read a link from your clipboard",
                this::importFromClipboard);
        addPageAction(content, "Profiles & subscriptions", "Manage accounts and endpoints",
                this::showProfileHub);
        addPageAction(content, "Language", "Use device language or choose manually",
                this::showLanguagePage);
        addPageAction(content, "Import a link", "Add an mbox:// profile or HTTPS subscription",
                this::showImportDialog);
        // Subscription operations are in Profiles; lab tools
        // remain accessible only from the seven-tap diagnostics easter egg.
    }

    private void showProfileHub() {
        LinearLayout content = openPage("Profiles", "Your current connection and subscriptions.",
                Page.PROFILES);
        String profileName = ProfileStore.hasProfile(this)
                ? ProfileStore.profileName(this) : ui("No profile imported");
        TextView active = new TextView(this);
        active.setText(ui("ACTIVE PROFILE") + "\n" + profileName);
        active.setTextSize(16);
        active.setTextColor(getColor(R.color.mbox_text));
        active.setPadding(dp(18), dp(20), dp(18), dp(20));
        GradientDrawable background = new GradientDrawable();
        background.setColor(getColor(R.color.mbox_surface));
        background.setCornerRadius(dp(17));
        background.setStroke(dp(1), getColor(R.color.mbox_border));
        active.setBackground(background);
        LinearLayout.LayoutParams params = new LinearLayout.LayoutParams(-1, -2);
        params.bottomMargin = dp(18);
        content.addView(active, params);

        addPageAction(content, "Manage all subscriptions", "Select, refresh or remove saved subscriptions",
                this::showSubscriptions);
        if (SubscriptionStore.isConfigured(this)) {
            addPageAction(content, "Refresh all subscriptions", "Download fresh profiles for all subscriptions",
                    () -> refreshSubscription(true));
        }
        addPageAction(content, "Choose endpoint", "Available while VPN is stopped",
                this::showSubscriptionEndpoints);
        addPageAction(content, "Paste profile / subscription", "Review before importing",
                this::showImportDialog);
        addPageAction(content, "Import from clipboard", "Read a link from your clipboard",
                this::importFromClipboard);
        addPageAction(content, "Add HTTPS subscription", "Import a URL",
                this::showSubscriptionDialog);
        if (SubscriptionStore.hasDirect(this)) {
            addPageAction(content, "Use direct profile", "Activate a saved direct profile", () -> {
                if (!vpnStopped()) return;
                try {
                    SubscriptionStore.activateDirect(this);
                    uiMessage = ui("Direct profile selected");
                    showProfileHub();
                } catch (Exception error) { showImportError(error); }
            });
        }
        if (SubscriptionStore.hasDirect(this) ||
                (SubscriptionStore.activeId(this).isEmpty() && ProfileStore.hasProfile(this))) {
            addPageAction(content, "Delete direct profile", "Remove local direct credentials with confirmation",
                    this::showDeleteDirectProfile);
        }
    }

    private void showLanguagePage() {
        LinearLayout content = openPage("Language selection",
                "The app follows your device language unless you choose otherwise.", Page.LANGUAGE);
        String selected = UiLanguage.selection(this);
        addPageAction(content, "System default" + ("system".equals(selected) ? "  ✓" : ""),
                "Follow device language", () -> selectLanguage("system"));
        addPageAction(content, "Russian" + ("ru".equals(selected) ? "  ✓" : ""),
                "Use Russian", () -> selectLanguage("ru"));
        addPageAction(content, "English" + ("en".equals(selected) ? "  ✓" : ""),
                "Use English", () -> selectLanguage("en"));
    }

    private void selectLanguage(String code) {
        if (!UiLanguage.set(this, code)) {
            uiMessage = "Failed to save language preference";
        }
        showSettingsHome(); // Redraw only the UI. Do not restart the tunnel.
    }

    private void showDeleteDirectProfile() {
        if (!vpnStopped()) return;
        LinearLayout content = openPage("Delete profile",
                "Remove this profile from this device only. Server credentials remain valid until revoked by an administrator.",
                Page.DELETE_PROFILE);
        android.widget.CheckBox confirm = new android.widget.CheckBox(this);
        confirm.setText(ui("I understand this removes local profile data"));
        confirm.setTextColor(getColor(R.color.mbox_text));
        confirm.setButtonTintList(android.content.res.ColorStateList.valueOf(getColor(R.color.mbox_accent)));
        confirm.setPadding(0, dp(14), 0, dp(22));
        content.addView(confirm);
        Button delete = new Button(this);
        delete.setText(ui("Delete local profile"));
        delete.setTextColor(getColor(R.color.mbox_on_danger));
        delete.setAllCaps(false);
        GradientDrawable danger = new GradientDrawable();
        danger.setColor(getColor(R.color.mbox_danger));
        danger.setCornerRadius(dp(16));
        delete.setBackground(danger);
        delete.setEnabled(false);
        confirm.setOnCheckedChangeListener((v, checked) -> delete.setEnabled(checked));
        delete.setOnClickListener(v -> {
            if (!confirm.isChecked() || !vpnStopped()) return;
            try {
                boolean activeDirect = SubscriptionStore.activeId(this).isEmpty();
                // If a subscription is active, remove only the saved direct
                // credential; never erase the active subscription runtime.
                SubscriptionStore.deleteSavedDirect(this);
                if (activeDirect) ProfileStore.deleteActiveProfile(this);
                uiMessage = ui("Profile removed");
                showProfileHub();
            } catch (Exception error) { showImportError(error); }
        });
        content.addView(delete, new LinearLayout.LayoutParams(-1, dp(56)));
    }

    private String diagnosticsSnapshot() {
        android.app.NotificationManager manager = (android.app.NotificationManager)
                getSystemService(NOTIFICATION_SERVICE);
        boolean notificationsAllowed = manager != null && manager.areNotificationsEnabled();
        android.app.NotificationChannel channel = manager == null ? null
                : manager.getNotificationChannel(MBoxVpnService.getNotificationChannelId());
        String channelStatus = channel == null ? "missing (VPN service not started)" :
                channel.getImportance() == android.app.NotificationManager.IMPORTANCE_NONE
                ? "blocked by channel settings" :
                channel.getImportance() <= android.app.NotificationManager.IMPORTANCE_LOW
                ? "silent (importance " + channel.getImportance() + ")" :
                "enabled (importance " + channel.getImportance() + ")";
        boolean posted = false;
        if (manager != null) {
            for (android.service.notification.StatusBarNotification notification :
                    manager.getActiveNotifications()) {
                if (notification.getId() == MBoxVpnService.getNotificationId()) {
                    posted = true;
                    break;
                }
            }
        }
        return "State: " + MBoxVpnService.getState()
                + "\nTransport: " + MBoxVpnService.getActiveTransport()
                + "\nPhase: " + MBoxVpnService.getConnectionPhase()
                + "\nConnection attempts: " + MBoxVpnService.getConnectionAttempts()
                + "\nRecovery attempts: " + MBoxVpnService.getRecoveryAttempts()
                + "\nConnected for: " + MBoxVpnService.getConnectedForSeconds() + " s"
                + "\nNotifications allowed: " + notificationsAllowed
                + "\nNotification channel: " + channelStatus
                + "\nNotification posted by app: " + posted
                + "\nLast error: " + MBoxVpnService.getLastError()
                + "\nPackets, bytes, streams: core counters not available yet"
                + "\n\nRead-only statistics since application process start.";
    }

    private void showDiagnostics() {
        LinearLayout content = openPage("Diagnostics", "Live, read-only connection status.",
                Page.DIAGNOSTICS);
        diagnosticsView = new TextView(this);
        diagnosticsView.setTextSize(14);
        diagnosticsView.setTextColor(getColor(R.color.mbox_text));
        diagnosticsView.setTextIsSelectable(true);
        diagnosticsView.setLineSpacing(dp(5), 1f);
        diagnosticsView.setPadding(dp(18), dp(20), dp(18), dp(20));
        GradientDrawable background = new GradientDrawable();
        background.setColor(getColor(R.color.mbox_surface));
        background.setCornerRadius(dp(18));
        background.setStroke(dp(1), getColor(R.color.mbox_border));
        diagnosticsView.setBackground(background);
        diagnosticsView.setText(diagnosticsSnapshot());
        content.addView(diagnosticsView, new LinearLayout.LayoutParams(-1, -2));
        if (AppRuntime.udpLabEnabled(this)) {
            addPageAction(content, "Transport laboratory", "Debug build only",
                    this::showLabTools);
        }
        lastDiagnosticsRedraw = android.os.SystemClock.elapsedRealtime();
    }

    // Available only from Diagnostics in debug builds; never the public Settings menu.
    private void showLabTools() {
        if (!AppRuntime.udpLabEnabled(this)) return;
        LinearLayout content = openPage("Transport laboratory", "Debug build only", Page.LAB);
        if (QuicLabTransport.configured(this))
            addPageAction(content, "Transport mode", "Choose transport (debug only)",
                    this::showTransportDialog);
        addPageAction(content, "UDP Lab: STUN through VPN", "Run debug UDP probe",
                this::runUdpProbe);
    }

    private void showSubscriptionDialog() {
        EditText input = new EditText(this);
        input.setHint(ui("https://your-server.example/api/v2/…"));
        input.setSingleLine(false);
        new AlertDialog.Builder(this, R.style.MBoxDialog).setTitle(ui("Add MBox subscription"))
                .setView(input).setNegativeButton(ui("Cancel"), null)
                .setPositiveButton(ui("Import"), (d,w) -> importSubscription(input.getText().toString().trim()))
                .show();
    }

    private boolean vpnStopped() {
        if (subscriptionConnecting) {uiMessage="Subscription update in progress";refreshUi();return false;}
        if (MBoxVpnService.getState() == MBoxVpnService.State.STOPPED) return true;
        uiMessage = "Disconnect VPN before changing profiles";
        refreshUi();
        return false;
    }

    private void importSubscription(String url) {
        if (!vpnStopped() || subscriptionConnecting) return;
        subscriptionConnecting = true;
        new Thread(() -> {
            try {
                String id = SubscriptionStore.importUrl(this, url);
                if (MBoxVpnService.getState() != MBoxVpnService.State.STOPPED)
                    throw new Exception("VPN started while downloading subscription");
                // Do not erase direct profiles or other subscriptions.
                SubscriptionStore.activate(this, id);
                handler.post(() -> {
                    subscriptionConnecting = false;
                    uiMessage = "Subscription saved";
                    refreshUi();
                    showSubscriptionEndpoints(id);
                });
            } catch (Exception ex) {
                handler.post(() -> { subscriptionConnecting = false; showImportError(ex); });
            }
        }, "MBox-subscribe").start();
    }

    private void showSubscriptions() {
        LinearLayout content = openPage("Subscriptions", "Manage and refresh your saved subscriptions.",
                Page.SUBSCRIPTIONS);
        try {
            final java.util.List<String> ids = SubscriptionStore.ids(this);
            final String[] labels = SubscriptionStore.subscriptionLabels(this);
            if (ids.isEmpty()) {
                addPageAction(content, "Add subscription", "Import another HTTPS subscription",
                        this::showSubscriptionDialog);
                return;
            }
            for (int i = 0; i < ids.size(); i++) {
                final String id = ids.get(i);
                addPageAction(content, labels[i], "Manage subscription",
                        () -> showSubscriptionActions(id));
            }
            addPageAction(content, "Refresh all subscriptions", "Download fresh profiles for all subscriptions",
                    () -> refreshSubscription(true));
            addPageAction(content, "Add subscription", "Import another HTTPS subscription",
                    this::showSubscriptionDialog);
        } catch (Exception error) { showImportError(error); }
    }

    private void showSubscriptionActions(String id) {
        final String[] choices = {"Use subscription", "Choose endpoint", "Refresh subscription", "Remove locally"};
        new AlertDialog.Builder(this, R.style.MBoxDialog).setTitle(ui("Subscription"))
                .setItems(choices, (dialog,which) -> {
                    switch(which) {
                        case 0:
                            if (!vpnStopped()) return;
                            try {
                                SubscriptionStore.activate(this,id);
                                uiMessage = "Subscription selected";
                                refreshUi();
                            } catch(Exception ex) { showImportError(ex); }
                            break;
                        case 1: showSubscriptionEndpoints(id);break;
                        case 2: refreshOneSubscription(id);break;
                        case 3:
                            if (!vpnStopped()) return;
                            new AlertDialog.Builder(this, R.style.MBoxDialog).setTitle(ui("Remove subscription?"))
                                    .setMessage(ui("The local subscription will be removed. This does not revoke the server link or delete your last active VPN profile."))
                                    .setNegativeButton(ui("Cancel"),null)
                                    .setPositiveButton(ui("Remove"), (d,w) -> {
                                        try { SubscriptionStore.remove(this,id);uiMessage="Subscription removed";refreshUi(); }
                                        catch(Exception ex) { showImportError(ex); }
                                    }).show();
                            break;
                    }
                }).setNegativeButton(ui("Close"),null).show();
    }

    private void showSubscriptionEndpoints() {
        String id = SubscriptionStore.activeId(this);
        if (id.isEmpty()) { showSubscriptions();return; }
        showSubscriptionEndpoints(id);
    }

    private void showSubscriptionEndpoints(String id) {
        if (!vpnStopped()) return;
        try {
            String[] items = SubscriptionStore.labels(this,id);
            new AlertDialog.Builder(this, R.style.MBoxDialog).setTitle(ui("Select MBox endpoint"))
                    .setItems(items, (d,which) -> {
                        try {
                            SubscriptionStore.selectEndpoint(this,id,which);
                            uiMessage = "Endpoint: " + items[which];refreshUi();
                        } catch(Exception ex) { showImportError(ex); }
                    }).setNegativeButton(ui("Cancel"),null).show();
        } catch(Exception ex) { showImportError(ex); }
    }

    private void refreshOneSubscription(String id) {
        if (!vpnStopped() || subscriptionConnecting) return;
        subscriptionConnecting = true;
        new Thread(() -> {
            try {
                SubscriptionStore.refreshOneForUi(this,id);
                if (MBoxVpnService.getState() != MBoxVpnService.State.STOPPED)
                    throw new Exception("VPN started while refreshing subscription");
                if (id.equals(SubscriptionStore.activeId(this))) SubscriptionStore.applySelected(this);
                handler.post(() -> {subscriptionConnecting=false;uiMessage="Subscription updated";refreshUi();});
            } catch(Exception ex) {
                handler.post(() -> {subscriptionConnecting=false;showImportError(ex);});
            }
        }, "MBox-sub-refresh-one").start();
    }

    private void refreshSubscription(boolean notify) {
        if (!SubscriptionStore.isConfigured(this) || subscriptionConnecting || !vpnStopped()) return;
        subscriptionConnecting = true;
        new Thread(() -> {
            try {
                if (MBoxVpnService.getState() != MBoxVpnService.State.STOPPED)
                    throw new Exception("VPN started while refreshing subscriptions");
                if (!notify) {
                    // Keep resume fast: refresh only the ACTIVE subscription.
                    // Other subscriptions are refreshed explicitly in Settings.
                    String id = SubscriptionStore.activeId(this);
                    if (!id.isEmpty()) {
                        SubscriptionStore.refreshOneForUi(this,id);
                        SubscriptionStore.applySelected(this);
                    }
                    handler.post(() -> {subscriptionConnecting=false;refreshUi();});
                } else {
                    int[] result = SubscriptionStore.refreshAll(this);
                    SubscriptionStore.applySelected(this);
                    handler.post(() -> {
                        subscriptionConnecting=false;
                        uiMessage = "Updated " + result[0] + ", failed " + result[1];
                        refreshUi();
                    });
                }
            } catch(Exception ex) {
                handler.post(() -> {subscriptionConnecting=false;if (notify) showImportError(ex);});
            }
        }, "MBox-sub-refresh").start();
    }

    private void showImportDialog() {
        EditText input = new EditText(this);
        input.setHint(ui("mbox://… or https://…/api/v2/…"));
        input.setSingleLine(false);
        input.setMinLines(2);
        input.setMaxLines(5);
        input.setInputType(InputType.TYPE_CLASS_TEXT |
                InputType.TYPE_TEXT_FLAG_MULTI_LINE |
                InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS);
        new AlertDialog.Builder(this, R.style.MBoxDialog)
                .setTitle(ui("Import MBox profile"))
                .setView(input)
                .setNegativeButton(ui("Cancel"), null)
                .setPositiveButton(ui("Review"), (d, w) -> importLink(input.getText().toString()))
                .show();
    }

    private void showTransportDialog() {
        if (!AppRuntime.udpLabEnabled(this)) return;
        if (MBoxVpnService.getState() != MBoxVpnService.State.STOPPED) {
            new AlertDialog.Builder(this, R.style.MBoxDialog)
                    .setMessage(ui("Disconnect the VPN before changing transport mode."))
                    .setPositiveButton(ui("OK"), null)
                    .show();
            return;
        }
        TransportPolicy.Mode[] modes = {
                TransportPolicy.Mode.AUTO, TransportPolicy.Mode.HTTPS, TransportPolicy.Mode.QUIC};
        TransportPolicy.Mode current = QuicLabTransport.mode(this);
        int selected = current == modes[0] ? 0 : current == modes[1] ? 1 : 2;
        new AlertDialog.Builder(this, R.style.MBoxDialog)
                .setTitle(ui("Transport mode"))
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
                .setNegativeButton(ui("Cancel"), null)
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
                new AlertDialog.Builder(this, R.style.MBoxDialog)
                        .setTitle(ui("MBox UDP laboratory"))
                        .setMessage(result + "\n\nA successful STUN response is not a leak audit.")
                        .setPositiveButton(ui("OK"), null)
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
        if (SubscriptionStore.looksLikeSubscription(value)) { importSubscription(value);return; }
        try {
            ProfileStore.Candidate candidate = ProfileStore.parseLink(value);
            String message = "Profile: " + candidate.name + "\nClient: " + candidate.clientId + "\nEndpoint: " + candidate.endpoint + "\n\nServer key SHA-256:\n" + candidate.serverFingerprint + "\n\nVerify this fingerprint over a trusted channel before replacing the active profile." +
                    (candidate.quicUrl.isEmpty() ? "" : "\n\nQUIC: " + candidate.quicUrl + "\nQUIC certificate SHA-256: " + candidate.quicCertificateFingerprint);
            Runnable commit = () -> {
                try { String name = ProfileStore.commitCandidate(this, candidate); SubscriptionStore.saveDirect(this, value); link.setText(ui("")); uiMessage = "Imported: " + name; }
                catch (Exception error) { showImportError(error); }
                refreshUi();
            };
            AlertDialog.Builder confirm = new AlertDialog.Builder(this, R.style.MBoxDialog).setTitle(ui("Confirm MBox profile")).setMessage(message).setNegativeButton(ui("Cancel"), null);
            if (candidate.requiresDangerousConsent()) {
                confirm.setPositiveButton(ui("Review unsafe options"), (dialog, which) -> new AlertDialog.Builder(this, R.style.MBoxDialog)
                        .setTitle(ui("Unsafe developer options"))
                        .setMessage(candidate.securityWarningText() + "\n\nThese options can expose metadata or create a stable traffic fingerprint. Import only for an explicit diagnostic session.")
                        .setNegativeButton(ui("Cancel"), null)
                        .setPositiveButton(ui("Import unsafe profile"), (d, w) -> commit.run())
                        .show());
            } else {
                confirm.setPositiveButton(ui("Replace profile"), (dialog, which) -> commit.run());
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
        new AlertDialog.Builder(this, R.style.MBoxDialog)
                .setTitle(ui("Profile import failed"))
                .setMessage(explanation)
                .setPositiveButton(ui("OK"), null)
                .show();
    }

    private void requestVpn() {
        if (subscriptionConnecting) {uiMessage="Subscription update in progress";refreshUi();return;}
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
        // Only when the verified VPN is connected; ReleaseUpdates binds to VPN.
        ReleaseUpdates.maybeCheck(this);
        if (currentPage == Page.DIAGNOSTICS && diagnosticsView != null) {
            long now = android.os.SystemClock.elapsedRealtime();
            if (now - lastDiagnosticsRedraw >= 2000) {
                lastDiagnosticsRedraw = now;
                diagnosticsView.setText(diagnosticsSnapshot());
            }
        }
        boolean hasProfile = ProfileStore.hasProfile(this);
        profile.setText(hasProfile ? ui("Profile: ") + ProfileStore.profileName(this) : ui("No profile"));
        subscriptionStatus.setText(SubscriptionStore.summary(this));

        MBoxVpnService.State vpnState = MBoxVpnService.getState();
        switch (vpnState) {
            case DISCONNECTED:
                status.setText(ui("Waiting for physical network…"));
                break;
            case CONNECTING:
                status.setText(ui("Connecting via ") + MBoxVpnService.getActiveTransport() +
                        "\n" + MBoxVpnService.getConnectionPhase());
                break;
            case VERIFYING:
                status.setText(ui("Verifying remote ") + MBoxVpnService.getActiveTransport() +
                        " session…\n" + MBoxVpnService.getConnectionPhase());
                break;
            case RECOVERING:
                status.setText(ui("Recovering ") + MBoxVpnService.getActiveTransport() +
                        "\n" + MBoxVpnService.getConnectionPhase() +
                        (MBoxVpnService.getLastError().isEmpty() ? "" : "\n" + ui("Cause: ") + MBoxVpnService.getLastError()));
                break;
            case CONNECTED:
                status.setText(ui("Connected via ") + MBoxVpnService.getActiveTransport() +
                        "\n" + MBoxVpnService.getConnectionPhase());
                break;
            case FAILED:
                status.setText(ui("Error: ") + MBoxVpnService.getLastError());
                break;
            case STOPPED:
            default:
                if (!uiMessage.isEmpty()) {
                    status.setText(uiMessage);
                } else if (!hasProfile) {
                    status.setText(ui("Import an mbox:// profile"));
                } else {
                    status.setText(ui("Ready"));
                }
                break;
        }

        // Reflect the live transport state without adding a separate UI model.
        status.setTextColor(vpnState == MBoxVpnService.State.CONNECTED
                ? getColor(R.color.mbox_accent)
                : vpnState == MBoxVpnService.State.FAILED
                ? Color.rgb(250, 150, 150)
                : Color.rgb(238, 243, 249));
        disconnectButton.setText(vpnState == MBoxVpnService.State.CONNECTED
                ? "Disconnect" : "Stop connection");

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
