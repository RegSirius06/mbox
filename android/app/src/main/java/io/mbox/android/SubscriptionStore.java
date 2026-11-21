package io.mbox.android;

import android.content.Context;
import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.StandardCopyOption;
import java.nio.file.AtomicMoveNotSupportedException;
import java.security.SecureRandom;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Locale;
import org.json.JSONArray;
import org.json.JSONObject;

/**
 * Several independently refreshable MBox subscriptions, each containing
 * multiple named endpoints. URL, PSKs and profile payloads are encrypted by
 * Android Keystore, not stored in SharedPreferences or Android backups.
 *
 * beta.4.2 legacy files are used in-place as subscription "legacy".
 * Keeping them in place avoids a fragile data migration on upgrade.
 */
final class SubscriptionStore {
    static final int MAX_BYTES = 60 * 1024;
    static final int MAX_SUBSCRIPTIONS = 12;
    private static final String PREFS = "mbox-subscription";
    private static final String ACTIVE = "active-subscription";
    private static final String OLD_ENDPOINT = "selected-id";
    private static final String URL = "subscription.url.enc";
    private static final String BUNDLE = "subscription.bundle.enc";
    private static final String LEGACY = "legacy";
    private static final String DIRECT_FILE = "direct.mbox.enc";
    private static final SecureRandom RANDOM = new SecureRandom();

    private SubscriptionStore() {}

    static File directory(Context c) { return new File(c.getFilesDir(), "subscriptions"); }
    private static File itemDir(Context c, String id) {
        if (!validId(id)) throw new IllegalArgumentException("Invalid subscription ID");
        return LEGACY.equals(id) ? directory(c) : new File(new File(directory(c), "items"), id);
    }
    private static boolean validId(String id) {
        return LEGACY.equals(id) || (id != null && id.matches("[0-9a-f]{32}"));
    }
    private static File urlFile(Context c, String id) { return new File(itemDir(c, id), URL); }
    private static File bundleFile(Context c, String id) { return new File(itemDir(c, id), BUNDLE); }
    private static android.content.SharedPreferences prefs(Context c) {
        return c.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
    }
    private static String endpointPref(String id) { return LEGACY.equals(id) ? OLD_ENDPOINT : "endpoint-" + id; }

    static List<String> ids(Context c) {
        ArrayList<String> found = new ArrayList<>();
        if (urlFile(c, LEGACY).isFile() && bundleFile(c, LEGACY).isFile()) found.add(LEGACY);
        File folder = new File(directory(c), "items");
        File[] children = folder.listFiles();
        if (children != null) for (File f : children) {
            if (f.isDirectory() && !Files.isSymbolicLink(f.toPath()) &&
                    validId(f.getName()) && urlFile(c, f.getName()).isFile() &&
                    bundleFile(c, f.getName()).isFile()) found.add(f.getName());
        }
        Collections.sort(found);
        return found;
    }
    static boolean isConfigured(Context c) { return !ids(c).isEmpty(); }
    static String activeId(Context c) {
        String id = prefs(c).getString(ACTIVE, "");
        if (id == null || !ids(c).contains(id)) return "";
        return id;
    }
    private static String selectedEndpoint(Context c, String id) {
        return prefs(c).getString(endpointPref(id), "");
    }

    static boolean looksLikeSubscription(String value) {
        if (value == null || value.length() > 2048) return false;
        try {
            java.net.URI uri = new java.net.URI(value.trim());
            if (!"https".equalsIgnoreCase(uri.getScheme()) || uri.getHost() == null ||
                    uri.getRawUserInfo() != null || uri.getRawFragment() != null ||
                    uri.getRawQuery() != null || uri.getPort() > 65535 ||
                    uri.getPort() == 0) return false;
            return uri.getRawPath() != null && uri.getRawPath().matches("(?:/[A-Za-z0-9_-]{1,64}){1,8}/[A-Za-z0-9_-]{42,48}");
        } catch (Exception ignored) { return false; }
    }

    private static String getUrl(Context c, String id) throws Exception {
        return CredentialVault.decryptFromFile(urlFile(c, id));
    }
    private static JSONObject cached(Context c, String id) throws Exception {
        JSONObject doc = new JSONObject(CredentialVault.decryptFromFile(bundleFile(c, id)));
        validate(doc);
        return doc;
    }
    private static JSONObject download(String url) throws Exception {
        if (!looksLikeSubscription(url)) throw new Exception("Ожидается HTTPS-ссылка подписки MBox");
        HttpURLConnection conn = null;
        try {
            conn = (HttpURLConnection) new URL(url).openConnection();
            conn.setInstanceFollowRedirects(false);
            conn.setRequestMethod("GET");
            conn.setConnectTimeout(7000);
            conn.setReadTimeout(7000);
            conn.setUseCaches(false);
            conn.setRequestProperty("Accept", "application/vnd.mbox.subscription.v2+json, application/json");
            int status = conn.getResponseCode();
            if (status != 200) throw new Exception("Сервер подписки вернул HTTP " + status);
            if (conn.getContentLength() > MAX_BYTES) throw new Exception("Слишком большая подписка");
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            try (InputStream stream = conn.getInputStream()) {
                byte[] buf = new byte[4096];
                int n;
                while ((n = stream.read(buf)) != -1) {
                    if (out.size() + n > MAX_BYTES) throw new Exception("Слишком большая подписка");
                    out.write(buf, 0, n);
                }
            }
            JSONObject d = new JSONObject(new String(out.toByteArray(), StandardCharsets.UTF_8));
            validate(d);
            return d;
        } finally { if (conn != null) conn.disconnect(); }
    }
    private static void validate(JSONObject doc) throws Exception {
        if (doc.getInt("version") != 1 && doc.getInt("version") != 2) throw new Exception("Неподдерживаемый формат подписки");
        JSONArray ps = doc.getJSONArray("profiles");
        if (ps.length() < 1 || ps.length() > 12) throw new Exception("Недопустимое число точек входа");
        String client = null, psk = null, server = null;
        ArrayList<String> ids = new ArrayList<>();
        for (int i = 0; i < ps.length(); i++) {
            JSONObject p = ps.getJSONObject(i);
            String eid = p.getString("id");
            if (!eid.matches("[A-Za-z0-9_-]{1,32}") || ids.contains(eid))
                throw new Exception("Повторяющийся ID точки входа");
            ids.add(eid);
            ProfileStore.Candidate candidate = ProfileStore.parseLink(p.getString("link"));
            if (candidate.requiresDangerousConsent())
                throw new Exception("Небезопасные диагностические настройки в подписке");
            if (client == null) {
                client = candidate.clientId;
                psk = candidate.psk;
                server = candidate.serverPublicKey;
            } else if (!client.equals(candidate.clientId) || !psk.equals(candidate.psk)
                    || !server.equals(candidate.serverPublicKey)) {
                throw new Exception("Входы подписки принадлежат разным пользователям или серверам");
            }
        }
    }

    private static void writeEncrypted(File destination, String plaintext) throws Exception {
        File dir = destination.getParentFile();
        if (!dir.isDirectory() && !dir.mkdirs()) throw new Exception("Не удалось создать каталог подписки");
        File temp = File.createTempFile(".mbox-", ".tmp", dir);
        boolean complete = false;
        try {
            CredentialVault.encryptToFile(temp, plaintext);
            try {
                Files.move(temp.toPath(), destination.toPath(), StandardCopyOption.REPLACE_EXISTING,
                        StandardCopyOption.ATOMIC_MOVE);
            } catch (AtomicMoveNotSupportedException e) {
                throw new Exception("Нет атомарной замены для защищённой подписки", e);
            }
            complete = true;
        } finally { if (!complete) temp.delete(); }
    }
    private static void save(Context c, String id, String url, JSONObject doc) throws Exception {
        // Commit the payload first, then its URL. Existing profiles stay usable
        // if a download or validation fails. No plaintext subscription on disk.
        writeEncrypted(bundleFile(c,id), doc.toString());
        writeEncrypted(urlFile(c,id), url);
    }
    private static String randomId(Context c) {
        for (int i = 0; i < 8; i++) {
            byte[] b = new byte[16]; RANDOM.nextBytes(b);
            StringBuilder s = new StringBuilder(32);
            for (byte v : b) s.append(String.format(Locale.ROOT, "%02x", v & 0xff));
            if (!itemDir(c, s.toString()).exists()) return s.toString();
        }
        throw new IllegalStateException("No unique subscription ID");
    }
    /** Return existing subscription ID when the URL was imported previously. */
    static String importUrl(Context c, String url) throws Exception {
        String trimmed = url == null ? "" : url.trim();
        if (!looksLikeSubscription(trimmed)) throw new Exception("Некорректная HTTPS-ссылка подписки");
        String existing = "";
        for (String id : ids(c)) if (trimmed.equals(getUrl(c,id))) { existing = id; break; }
        JSONObject doc = download(trimmed); // Authenticate/validate before touching storage.
        if (!existing.isEmpty()) {
            checkIdentity(c, existing, doc);
            save(c,existing,trimmed,doc);
            return existing;
        }
        if (ids(c).size() >= MAX_SUBSCRIPTIONS) throw new Exception("Максимум 12 подписок");
        String id = randomId(c);
        save(c,id,trimmed,doc);
        return id;
    }
    private static void checkIdentity(Context c, String id, JSONObject newDoc) throws Exception {
        JSONArray oldPs = cached(c,id).getJSONArray("profiles");
        JSONArray newPs = newDoc.getJSONArray("profiles");
        ProfileStore.Candidate old = ProfileStore.parseLink(oldPs.getJSONObject(0).getString("link"));
        ProfileStore.Candidate current = ProfileStore.parseLink(newPs.getJSONObject(0).getString("link"));
        if (!old.clientId.equals(current.clientId) || !old.serverPublicKey.equals(current.serverPublicKey))
            throw new Exception("Сменилась идентичность Noise; требуется ручной переимпорт");
    }
    static void refreshOneForUi(Context c, String id) throws Exception {
        refreshOne(c, id);
    }
    private static void refreshOne(Context c, String id) throws Exception {
        String url = getUrl(c,id);
        JSONObject doc = download(url);
        checkIdentity(c,id,doc);
        save(c,id,url,doc);
    }
    /** Returns updated/failed counts. A failed item retains its cached copy. */
    static int[] refreshAll(Context c) throws Exception {
        int ok = 0, errors = 0;
        for (String id : ids(c)) {
            try { refreshOne(c,id);ok++; }
            catch (Exception exc) { errors++; }
        }
        return new int[]{ok,errors};
    }
    static void refresh(Context c) throws Exception { // compatibility with beta.4.2
        String id = activeId(c);
        if (!id.isEmpty()) refreshOne(c,id);
        else if (!ids(c).isEmpty()) refreshOne(c,ids(c).get(0));
        else throw new Exception("Нет подписок");
    }

    static String[] subscriptionLabels(Context c) throws Exception {
        List<String> list = ids(c);
        String[] labels = new String[list.size()];
        for (int i = 0; i < list.size(); i++) {
            String id = list.get(i);
            JSONObject doc = cached(c,id);
            JSONArray ps = doc.getJSONArray("profiles");
            ProfileStore.Candidate candidate = ProfileStore.parseLink(ps.getJSONObject(0).getString("link"));
            String prefix = id.equals(activeId(c)) ? "● " : "";
            String name = candidate.name;
            if (name.length() > 50) name = name.substring(0, 50);
            labels[i] = prefix + name + " · " + ps.length() + " профилей";
        }
        return labels;
    }
    static String[] labels(Context c, String id) throws Exception {
        JSONArray ps = cached(c,id).getJSONArray("profiles");
        String[] labels = new String[ps.length()];
        for (int i = 0; i < labels.length; i++) {
            JSONObject p = ps.getJSONObject(i);
            String name = p.optString("name", "Вход " + (i + 1));
            labels[i] = (p.getString("id").equals(selectedEndpoint(c,id)) ? "● " : "") + name;
        }
        return labels;
    }
    /** Activate selected endpoint by generating the same runtime config as mbox://. */
    static void selectEndpoint(Context c, String id, int index) throws Exception {
        JSONArray ps = cached(c,id).getJSONArray("profiles");
        if (index < 0 || index >= ps.length()) throw new Exception("Нет такого входа");
        JSONObject entry = ps.getJSONObject(index);
        ProfileStore.Candidate candidate = ProfileStore.parseLink(entry.getString("link"));
        if (candidate.requiresDangerousConsent()) throw new Exception("Небезопасный профиль");
        ProfileStore.commitCandidate(c,candidate);
        prefs(c).edit().putString(endpointPref(id),entry.getString("id"))
                .putString(ACTIVE,id).commit();
    }
    static void activate(Context c, String id) throws Exception {
        JSONArray ps = cached(c,id).getJSONArray("profiles");
        String selected = selectedEndpoint(c,id);
        int index = selected.isEmpty() ? 0 : -1;
        for (int i = 0; i < ps.length(); i++)
            if (ps.getJSONObject(i).getString("id").equals(selected)) { index = i; break; }
        // Never silently switch countries if a previously selected exit vanished.
        if (index < 0) throw new Exception("Выбранная страна больше не доступна. Выберите профиль вручную.");
        selectEndpoint(c,id,index);
    }
    static void applySelected(Context c) throws Exception {
        String id = activeId(c);
        if (!id.isEmpty()) activate(c,id); // Direct-imported profile stays active otherwise.
    }
    static void saveDirect(Context c, String raw) throws Exception {
        String link = ProfileStore.extractProvisioningLink(raw);
        ProfileStore.parseLink(link); // Never persist an unverified import.
        writeEncrypted(new File(directory(c),DIRECT_FILE), link);
        deactivate(c);
    }
    static boolean hasDirect(Context c) { return new File(directory(c),DIRECT_FILE).isFile(); }
    static void activateDirect(Context c) throws Exception {
        String link = CredentialVault.decryptFromFile(new File(directory(c),DIRECT_FILE));
        ProfileStore.Candidate candidate = ProfileStore.parseLink(link);
        ProfileStore.commitCandidate(c,candidate);
        deactivate(c);
    }
    /** Remove only the encrypted saved direct link, never any subscription token. */
    static void deleteSavedDirect(Context c) throws Exception {
        File file = new File(directory(c), DIRECT_FILE);
        if (file.exists() && !file.delete())
            throw new java.io.IOException("Cannot delete saved direct profile");
    }

    static void deactivate(Context c) { prefs(c).edit().remove(ACTIVE).commit(); }
    static void clear(Context c) { deactivate(c); } // no deletion on direct import

    static void remove(Context c, String id) throws Exception {
        if (!ids(c).contains(id)) throw new Exception("Подписка не найдена");
        if (id.equals(activeId(c))) deactivate(c); // retain active local runtime as direct
        File url = urlFile(c,id), bundle = bundleFile(c,id);
        if (!url.delete() || !bundle.delete()) throw new Exception("Не удалось удалить локальную подписку");
        if (!LEGACY.equals(id)) itemDir(c,id).delete();
        prefs(c).edit().remove(endpointPref(id)).apply();
    }
    static String summary(Context c) {
        List<String> list = ids(c);
        if (list.isEmpty()) return "";
        String id = activeId(c);
        if (id.isEmpty()) return UiLanguage.text(c,
                list.size() + " subscriptions · direct profile active",
                list.size() + " подписок · активен прямой профиль");
        try {
            JSONObject account = cached(c,id).optJSONObject("account");
            if (account == null) return UiLanguage.text(c,
                    list.size() + " subscriptions · active selected",
                    list.size() + " подписок · активная выбрана");
            String expires = account.isNull("expires_at") ? "" : account.optString("expires_at", "");
            int sessions = account.optInt("session_limit",0);
            String limitEn = sessions == -1 ? "∞" : sessions == 0 ? "default" : String.valueOf(sessions);
            String limitRu = sessions == -1 ? "∞" : sessions == 0 ? "по умолчанию" : String.valueOf(sessions);
            return UiLanguage.text(c,
                    list.size() + " subscriptions · sessions: " + limitEn + " · until " + (expires.isEmpty() ? "∞" : expires),
                    list.size() + " подписок · сессии: " + limitRu + " · до " + (expires.isEmpty() ? "∞" : expires));
        } catch(Exception ignored) {
            return UiLanguage.text(c, list.size() + " subscriptions", list.size() + " подписок");
        }
    }
}
