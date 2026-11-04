package io.mbox.android;

import java.util.regex.Matcher;
import java.util.regex.Pattern;

/** Redacts untrusted exception/core text before passing it to Android logcat. */
final class MBoxLogSanitizer {
    private static final int MAX_LINE = 4096;
    private static final Pattern URL = Pattern.compile("(?i)(?:https?|mbox)://[^\\s\\\"'<>\\\\]+", Pattern.CASE_INSENSITIVE);
    private static final Pattern BEARER = Pattern.compile("(?i)\\bbearer\\s+[A-Za-z0-9._~+/-]+");
    private static final Pattern KEY_VALUE = Pattern.compile(
            "(?i)\\b(psk|password|token|secret|authorization|private_key)\\s*[:=]\\s*[^\\s,;]+");

    private MBoxLogSanitizer() {}

    static String sanitize(String input) {
        if (input == null) return "";
        String text = input.substring(0, Math.min(input.length(), MAX_LINE));
        text = text.replace("\r", "\\r").replace("\n", "\\n");
        text = URL.matcher(text).replaceAll("[URL_REDACTED]");
        text = BEARER.matcher(text).replaceAll("Bearer [REDACTED]");
        Matcher matcher = KEY_VALUE.matcher(text);
        StringBuffer redacted = new StringBuffer();
        while (matcher.find()) {
            String match = matcher.group();
            int colon = match.indexOf(':');
            int equal = match.indexOf('=');
            int sep = colon < 0 ? equal : equal < 0 ? colon : Math.min(colon, equal);
            // Security invariant: preserve only the key label, never its value.
            String replacement = sep < 0 ? "[SECRET_REDACTED]" : match.substring(0, sep + 1) + "[REDACTED]";
            matcher.appendReplacement(redacted, Matcher.quoteReplacement(replacement));
        }
        matcher.appendTail(redacted);
        return redacted.toString().replaceAll("[\\x00-\\x08\\x0B\\x0C\\x0E-\\x1F]", "?");
    }
}
