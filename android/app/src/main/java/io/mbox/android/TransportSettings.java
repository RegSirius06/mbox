package io.mbox.android;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.Context;
import android.content.SharedPreferences;
import android.text.InputType;
import android.view.View;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.RadioButton;
import android.widget.RadioGroup;
import android.widget.ScrollView;
import android.widget.Switch;
import android.widget.TextView;
import android.widget.Toast;

/** Release-safe preferences, deliberately separate from old debug lab switches. */
final class TransportSettings {
    private static final String PREFS = "mbox-transport-settings";
    private static final String MODE = "mode-v2";
    private static final String INITIAL = "initial-seconds";
    private static final String MAXIMUM = "maximum-seconds";
    private static final String BACKOFF = "exponential-backoff";
    private static final String FAILBACK = "auto-failback";
    private static final String HTTPS_FIRST = "https-first";

    private static SharedPreferences prefs(Context c) {
        return c.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }

    static TransportPolicy.Mode mode(Context c) {
        String value = prefs(c).getString(MODE, null);
        if (value == null) return null;
        try { return TransportPolicy.Mode.valueOf(value); }
        catch (IllegalArgumentException invalid) { return TransportPolicy.Mode.AUTO; }
    }

    static void setMode(Context c, TransportPolicy.Mode mode) {
        if (!prefs(c).edit().putString(MODE, mode.name()).commit())
            throw new IllegalStateException("Cannot persist transport selection");
    }

    private static int initial(SharedPreferences p) {
        return Math.max(30, Math.min(300, p.getInt(INITIAL, 60)));
    }

    private static int maximum(SharedPreferences p) {
        return Math.max(initial(p), Math.min(1800, p.getInt(MAXIMUM, 900)));
    }

    // Stable QUIC feature, enabled automatically whenever QUIC is configured.
    // HTTPS does not start a DATAGRAM lane.
    enum UdpMode { AUTO, STREAMS, DATAGRAMS }
    private static final String UDP_MODE = "udp-mode-v1";

    static UdpMode udpMode(Context c) {
        try { return UdpMode.valueOf(prefs(c).getString(UDP_MODE, UdpMode.AUTO.name())); }
        catch (IllegalArgumentException invalid) { return UdpMode.AUTO; }
    }

    // Capability and user selection are separate: datagrams() reports whether
    // this profile has a QUIC gateway; datagramsEnabled() applies the hidden mode.
    static boolean datagrams(Context c) { return QuicLabTransport.configured(c); }

    static boolean datagramsEnabled(Context c) {
        return datagrams(c) && udpMode(c) != UdpMode.STREAMS;
    }

    // Only exposed through the hidden diagnostics window (seven logo taps).
    static void showUdpMode(Activity a) {
        if (!stopped(a)) return;
        if (!QuicLabTransport.configured(a)) {
            Toast.makeText(a, tr(a, "QUIC is not configured", "QUIC не настроен"), Toast.LENGTH_LONG).show();
            return;
        }
        final UdpMode[] values = UdpMode.values();
        final String[] labels = {
                tr(a, "AUTO — DATAGRAM with STREAMS fallback (recommended)",
                        "AUTO — DATAGRAM с резервом STREAMS (рекомендуется)"),
                tr(a, "STREAMS only — reliable UDP framing",
                        "Только STREAMS — надёжные UDP-кадры"),
                tr(a, "DATAGRAM only — UDP can be lost; updated origin required",
                        "Только DATAGRAM — UDP может теряться; нужен новый origin")
        };
        new AlertDialog.Builder(a, R.style.MBoxDialog)
                .setTitle(tr(a, "UDP routing (developer)", "Передача UDP (разработчик)"))
                .setSingleChoiceItems(labels, udpMode(a).ordinal(), (dialog, selected) -> {
                    if (!stopped(a)) { dialog.dismiss(); return; }
                    if (!prefs(a).edit().putString(UDP_MODE, values[selected].name()).commit()) {
                        Toast.makeText(a, tr(a, "Could not save setting", "Не удалось сохранить"), Toast.LENGTH_LONG).show();
                        return;
                    }
                    dialog.dismiss();
                    Toast.makeText(a, tr(a, "Applies at next VPN connection",
                            "Вступит в силу при следующем подключении VPN"), Toast.LENGTH_SHORT).show();
                }).setNegativeButton(tr(a, "Cancel", "Отмена"), null).show();
    }

    static void configure(Context c, TransportPolicy policy) {
        SharedPreferences p = prefs(c);
        policy.configure(initial(p) * 1000L, maximum(p) * 1000L,
                p.getBoolean(BACKOFF, true), p.getBoolean(FAILBACK, true),
                p.getBoolean(HTTPS_FIRST, false) ? TransportPolicy.Path.HTTPS : TransportPolicy.Path.QUIC);
    }

    static void show(Activity a) {
        if (!stopped(a)) return;
        SharedPreferences p = prefs(a);
        LinearLayout box = new LinearLayout(a);
        box.setOrientation(LinearLayout.VERTICAL);
        int pad = (int) (20 * a.getResources().getDisplayMetrics().density);
        box.setPadding(pad, pad / 2, pad, pad / 2);
        ScrollView scroll = new ScrollView(a);
        scroll.addView(box);

        RadioGroup group = new RadioGroup(a);
        TransportPolicy.Mode[] options = {TransportPolicy.Mode.AUTO, TransportPolicy.Mode.HTTPS, TransportPolicy.Mode.QUIC};
        int[] ids = new int[options.length];
        boolean quicConfigured = QuicLabTransport.configured(a);
        TransportPolicy.Mode prior = mode(a);
        if (prior == null) prior = TransportPolicy.Mode.AUTO;
        for (int i = 0; i < options.length; i++) {
            RadioButton button = new RadioButton(a);
            ids[i] = View.generateViewId();
            button.setId(ids[i]);
            button.setText(i == 0 ? tr(a, "Auto (recommended)", "Авто (рекомендуется)") : options[i].name());
            if (options[i] == TransportPolicy.Mode.QUIC && !quicConfigured) button.setEnabled(false);
            group.addView(button);
            if (prior == options[i]) group.check(ids[i]);
        }
        box.addView(group);
        Switch httpsFirst = toggle(a, box, tr(a, "Start AUTO with HTTPS (off: QUIC)",
                "Начинать AUTO с HTTPS (выкл.: QUIC)"), p.getBoolean(HTTPS_FIRST, false));
        httpsFirst.setEnabled(quicConfigured && group.getCheckedRadioButtonId() == ids[0]);
        group.setOnCheckedChangeListener((g, id) -> httpsFirst.setEnabled(quicConfigured && id == ids[0]));
        Switch failback = toggle(a, box, tr(a, "Automatic QUIC failback", "Автовозврат на QUIC"), p.getBoolean(FAILBACK, true));
        Switch exponential = toggle(a, box, tr(a, "Exponential backoff", "Экспоненциальный backoff"), p.getBoolean(BACKOFF, true));
        EditText initial = number(a, box, tr(a, "Initial interval (30–300 s)", "Начальный интервал (30–300 с)"), initial(p));
        EditText maximum = number(a, box, tr(a, "Maximum interval (up to 1800 s)", "Максимальный интервал (до 1800 с)"), maximum(p));
        TextView message = new TextView(a);
        message.setText(tr(a, "Changes apply at the next connection. QUIC probes do not authenticate an MBox session.",
                "Настройки действуют со следующего подключения. Проверка QUIC не аутентифицирует сессию MBox."));
        box.addView(message);
        AlertDialog dialog = new AlertDialog.Builder(a, R.style.MBoxDialog)
                .setTitle(tr(a, "Transport settings", "Настройки транспорта"))
                .setView(scroll).setPositiveButton(tr(a, "Save", "Сохранить"), null)
                .setNeutralButton(tr(a, "Defaults", "По умолчанию"), null)
                .setNegativeButton(tr(a, "Cancel", "Отмена"), null).create();
        dialog.setOnShowListener(ignored -> {
            dialog.getButton(AlertDialog.BUTTON_NEUTRAL).setOnClickListener(v -> {
                group.check(ids[0]); httpsFirst.setChecked(false);
                failback.setChecked(true); exponential.setChecked(true);
                initial.setText("60"); maximum.setText("900");
            });
            dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(v -> {
                if (!stopped(a)) { dialog.dismiss(); return; }
                int i, m;
                try {
                    i = Integer.parseInt(initial.getText().toString());
                    m = Integer.parseInt(maximum.getText().toString());
                    if (i < 30 || i > 300 || m < i || m > 1800) throw new NumberFormatException();
                } catch (NumberFormatException invalid) {
                    message.setText(tr(a, "Invalid retry intervals", "Недопустимые интервалы проверок"));
                    return;
                }
                TransportPolicy.Mode selected = TransportPolicy.Mode.AUTO;
                for (int n = 0; n < ids.length; n++) if (group.getCheckedRadioButtonId() == ids[n]) selected = options[n];
                if (selected == TransportPolicy.Mode.QUIC && !QuicLabTransport.configured(a)) {
                    message.setText(tr(a, "QUIC is not configured in this profile", "QUIC не настроен в этом профиле"));
                    return;
                }
                if (!p.edit().putString(MODE, selected.name()).putInt(INITIAL, i).putInt(MAXIMUM, m)
                        .putBoolean(BACKOFF, exponential.isChecked()).putBoolean(FAILBACK, failback.isChecked())
                        .putBoolean(HTTPS_FIRST, httpsFirst.isChecked()).commit()) {
                    message.setText(tr(a, "Could not save settings", "Не удалось сохранить настройки"));
                    return;
                }
                dialog.dismiss();
            });
        });
        dialog.show();
    }

    private static boolean stopped(Activity a) {
        if (MBoxVpnService.getState() == MBoxVpnService.State.STOPPED) return true;
        Toast.makeText(a, tr(a, "Disconnect VPN before changing transport settings", "Отключите VPN перед изменением настроек транспорта"), Toast.LENGTH_LONG).show();
        return false;
    }

    private static Switch toggle(Activity a, LinearLayout box, String title, boolean checked) {
        Switch control = new Switch(a);
        control.setText(title);
        control.setChecked(checked);
        box.addView(control);
        return control;
    }

    private static EditText number(Activity a, LinearLayout box, String title, int value) {
        TextView label = new TextView(a);
        label.setText(title);
        box.addView(label);
        EditText field = new EditText(a);
        field.setInputType(InputType.TYPE_CLASS_NUMBER);
        field.setSingleLine(true);
        field.setText(Integer.toString(value));
        box.addView(field);
        return field;
    }

    private static String tr(Context c, String en, String ru) { return UiLanguage.text(c, en, ru); }
    private TransportSettings() {}
}
