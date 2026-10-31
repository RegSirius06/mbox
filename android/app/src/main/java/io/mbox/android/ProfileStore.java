package io.mbox.android;

import android.content.Context;
import android.util.Base64;

import org.json.JSONObject;

import java.io.File;
import java.io.FileOutputStream;
import java.net.URI;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.util.Locale;

/** Imports a portable mbox:// link into files private to this Android app. */
public final class ProfileStore {
    private static final String PREFS = "mbox-profile";
    private static final String PREF_NAME = "name";

    public static File profileDir(Context context) {
        return new File(context.getFilesDir(), "profile");
    }

    public static File configFile(Context context) {
        return new File(profileDir(context), "client.json");
    }

    public static boolean hasProfile(Context context) {
        return configFile(context).isFile();
    }

    public static String profileName(Context context) {
        return context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                .getString(PREF_NAME, "MBox");
    }

    private static void atomicWrite(File destination, String text) throws Exception {
        File parent = destination.getParentFile();
        if (parent == null || (!parent.isDirectory() && !parent.mkdirs())) {
            throw new Exception("Не удалось создать каталог профиля");
        }

        File temporary = new File(parent, destination.getName() + ".tmp");
        try (FileOutputStream out = new FileOutputStream(temporary, false)) {
            out.write(text.getBytes(StandardCharsets.UTF_8));
            out.getFD().sync();
        }

        // filesDir is already private to the application UID. These calls make
        // the intent explicit for devices with unusual default umasks.
        temporary.setReadable(false, false);
        temporary.setWritable(false, false);
        temporary.setReadable(true, true);
        temporary.setWritable(true, true);

        if (destination.exists() && !destination.delete()) {
            throw new Exception("Не удалось заменить старый профиль");
        }
        if (!temporary.renameTo(destination)) {
            throw new Exception("Не удалось сохранить профиль");
        }
    }

    private static boolean isHex32(String value) {
        return value != null && value.matches("[0-9a-fA-F]{64}");
    }

    private static boolean validClientId(String id) {
        if (id == null || id.isEmpty() || id.length() > 63) {
            return false;
        }
        for (int i = 0; i < id.length(); i++) {
            char c = id.charAt(i);
            if (!(c >= 'a' && c <= 'z') &&
                    !(c >= 'A' && c <= 'Z') &&
                    !(c >= '0' && c <= '9') &&
                    c != '.' && c != '_' && c != '-') {
                return false;
            }
        }
        return true;
    }

    private static void validateBaseUrl(String raw) throws Exception {
        URI uri = new URI(raw);
        String scheme = uri.getScheme();
        String host = uri.getHost();

        if (scheme == null || host == null ||
                uri.getRawUserInfo() != null || uri.getRawQuery() != null ||
                uri.getRawFragment() != null || raw.endsWith("/")) {
            throw new Exception("Некорректный transport.url");
        }

        if ("https".equalsIgnoreCase(scheme)) {
            return;
        }

        boolean loopback = "127.0.0.1".equals(host) || "::1".equals(host) ||
                "localhost".equalsIgnoreCase(host);
        if (!"http".equalsIgnoreCase(scheme) || !loopback) {
            throw new Exception("Удалённый MBox endpoint должен использовать HTTPS");
        }
    }


    public static String transportHost(Context context) throws Exception {
        byte[] data = java.nio.file.Files.readAllBytes(configFile(context).toPath());
        JSONObject root = new JSONObject(new String(data, StandardCharsets.UTF_8));
        String endpoint = root.getJSONObject("transport").getString("url");
        URI uri = new URI(endpoint);
        String host = uri.getHost();
        if (host == null || host.isEmpty()) {
            throw new Exception("В профиле отсутствует hostname MBox endpoint");
        }
        return host;
    }

    public static String importLink(Context context, String link) throws Exception {
        String input = link == null ? "" : link.trim();
        final String scheme = "mbox://";
        if (!input.regionMatches(true, 0, scheme, 0, scheme.length())) {
            throw new Exception("Это не mbox:// ссылка");
        }

        String payload = input.substring(scheme.length());
        String fragment = "";
        int hash = payload.indexOf('#');
        if (hash >= 0) {
            fragment = payload.substring(hash + 1);
            payload = payload.substring(0, hash);
        }

        // A copied link may contain visual line breaks from a terminal or messenger.
        // Whitespace is not meaningful inside the base64url payload, so ignore it.
        payload = payload.replaceAll("\\s+", "");

        byte[] decoded = Base64.decode(payload, Base64.URL_SAFE | Base64.NO_WRAP | Base64.NO_PADDING);
        JSONObject root = new JSONObject(new String(decoded, StandardCharsets.UTF_8));

        if (root.getInt("v") != 1) {
            throw new Exception("Неподдерживаемая версия MBox link");
        }

        String clientId = root.optString("client_id", "");
        if (!validClientId(clientId)) {
            throw new Exception("Некорректный client_id");
        }

        JSONObject transport = root.getJSONObject("transport");
        JSONObject crypto = root.getJSONObject("crypto");
        JSONObject camouflage = root.getJSONObject("camouflage");

        String endpoint = transport.getString("url");
        String psk = crypto.getString("psk");
        String serverPublicKey = crypto.getString("server_public_key");

        validateBaseUrl(endpoint);
        if (!isHex32(psk)) {
            throw new Exception("Некорректный PSK");
        }
        if (!isHex32(serverPublicKey)) {
            throw new Exception("Некорректный server public key");
        }

        File dir = profileDir(context);
        File pskFile = new File(dir, "mbox.psk");
        File pubFile = new File(dir, "mbox-server.pub");

        atomicWrite(pskFile, psk.toLowerCase(Locale.ROOT) + "\n");
        atomicWrite(pubFile, serverPublicKey.toLowerCase(Locale.ROOT) + "\n");

        JSONObject diskCrypto = new JSONObject();
        diskCrypto.put("client_id", clientId);
        diskCrypto.put("psk_file", pskFile.getAbsolutePath());
        diskCrypto.put("server_public_key_file", pubFile.getAbsolutePath());

        JSONObject diskTransport = new JSONObject();
        diskTransport.put("listen", AppRuntime.socksListen());
        diskTransport.put("url", endpoint);

        JSONObject disk = new JSONObject();
        disk.put("camouflage", camouflage);
        disk.put("transport", diskTransport);
        disk.put("crypto", diskCrypto);
        atomicWrite(configFile(context), disk.toString(2) + "\n");

        String name = root.optString("name", "");
        if (name.isEmpty() && !fragment.isEmpty()) {
            name = URLDecoder.decode(fragment, StandardCharsets.UTF_8.name());
        }
        if (name.isEmpty()) {
            name = clientId;
        }

        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                .edit().putString(PREF_NAME, name).apply();
        return name;
    }

    private ProfileStore() {}
}
