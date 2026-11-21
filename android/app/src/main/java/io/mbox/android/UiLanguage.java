package io.mbox.android;

import android.content.Context;
import java.util.HashMap;
import java.util.Locale;
import java.util.Map;

/** Small, app-local EN/RU preference; does not change VPN configuration or Android's global locale. */
final class UiLanguage {
    private static final String PREFS = "mbox-ui-language";
    private static final String KEY = "selection";
    private static final Map<String, String> RU = new HashMap<>();

    static {
        String[][] entries = {
            {"GitHub · Releases", "GitHub · Релизы"},
            {"Telegram community", "Сообщество в Telegram"},
            {"Telegram · Community", "Telegram · Сообщество"},
            {"No browser available", "Не найден браузер"},
            {"MBox", "MBox"}, {"PRIVATE CONNECTION", "ЗАЩИЩЁННОЕ СОЕДИНЕНИЕ"},
            {"Your connection, under your control.", "Ваше соединение под вашим контролем."},
            {"Connect", "Подключиться"}, {"Disconnect", "Отключиться"},
            {"Stop connection", "Остановить подключение"},
            {"Profiles & settings", "Профили и настройки"},
            {"Choose profile", "Выбрать профиль"},
            {"Choose subscription", "Выбрать подписку"},
            {"Update profile", "Обновить профиль"},
            {"Updating profile…", "Обновление профиля…"},
            {"Import profile", "Импортировать профиль"},
            {"Profile: ", "Профиль: "}, {"No profile", "Нет профиля"},
            {"No profile imported", "Профиль не импортирован"},
            {"Import an mbox:// profile", "Импортируйте профиль mbox://"},
            {"Ready", "Готово"}, {"Waiting for physical network…", "Ожидание мобильной сети или Wi-Fi…"},
            {"Connecting via ", "Подключение через "}, {"Connected via ", "Подключено через "},
            {"Recovering ", "Восстановление "}, {"Error: ", "Ошибка: "},
            {"Verifying remote ", "Проверка удалённой сессии "},
            {" session…\n", "…\n"}, {"Cause: ", "Причина: "},
            {"‹  Back", "‹  Назад"},
            {"Settings", "Настройки"}, {"Everything you need, in one place.", "Всё необходимое в одном месте."},
            {"Profiles & subscriptions", "Профили и подписки"},
            {"Manage accounts and endpoints", "Управление профилями и точками входа"},
            {"Import a link", "Импортировать ссылку"},
            {"Add an mbox:// profile or HTTPS subscription", "Добавить профиль mbox:// или HTTPS-подписку"},
            {"Language", "Язык"}, {"Use device language or choose manually", "Язык устройства или ручной выбор"},
            {"Profiles", "Профили"}, {"Your current connection and subscriptions.", "Ваши подключения и подписки."},
            {"ACTIVE PROFILE", "АКТИВНЫЙ ПРОФИЛЬ"}, {"Subscriptions", "Подписки"},
            {"View and switch saved subscriptions", "Просмотреть и переключить подписки"},
            {"Manage all subscriptions", "Управление подписками"},
            {"Select, refresh or remove saved subscriptions", "Выбрать, обновить или удалить сохранённые подписки"},
            {"Refresh all subscriptions", "Обновить все подписки"},
            {"Download fresh profiles for all subscriptions", "Получить свежие профили всех подписок"},
            {"Choose endpoint", "Выбрать точку входа"},
            {"Available while VPN is stopped", "Доступно при выключенном VPN"},
            {"Paste profile / subscription", "Вставить профиль или подписку"},
            {"Review before importing", "Проверка перед импортом"},
            {"Import from clipboard", "Импортировать из буфера"},
            {"Read a link from your clipboard", "Получить ссылку из буфера обмена"},
            {"Add HTTPS subscription", "Добавить HTTPS-подписку"},
            {"Import a URL", "Импортировать URL"},
            {"Use direct profile", "Выбрать прямой профиль"},
            {"Activate a saved direct profile", "Включить сохранённый прямой профиль"},
            {"Delete direct profile", "Удалить прямой профиль"},
            {"Remove local direct credentials with confirmation", "Удалить локальные данные с подтверждением"},
            {"Subscriptions", "Подписки"},
            {"Manage and refresh your saved subscriptions.", "Управление и обновление сохранённых подписок."},
            {"Manage subscription", "Управление подпиской"},
            {"Add subscription", "Добавить подписку"},
            {"Import another HTTPS subscription", "Импортировать ещё одну HTTPS-подписку"},
            {"Delete profile", "Удаление профиля"},
            {"Remove this profile from this device only. Server credentials remain valid until revoked by an administrator.",
             "Удалить профиль только с этого устройства. Серверный доступ продолжит действовать до отзыва администратором."},
            {"I understand this removes local profile data", "Я понимаю, что локальные данные профиля будут удалены"},
            {"Delete local profile", "Удалить локальный профиль"},
            {"Profile removed", "Профиль удалён"},
            {"Language selection", "Выбор языка"},
            {"The app follows your device language unless you choose otherwise.",
             "По умолчанию используется язык устройства."},
            {"System default", "Как на устройстве"},
            {"Follow device language", "Следовать языку устройства"},
            {"Russian", "Русский"}, {"English", "Английский"},
            {"Use Russian", "Использовать русский"}, {"Use English", "Использовать английский"},
            {"Diagnostics", "Диагностика"}, {"Live, read-only connection status.", "Текущее состояние соединения, только чтение."},
            {"Connection status and advanced settings.", "Состояние соединения и расширенные настройки."},
            {"Transport settings", "Настройки транспорта"},
            {"Transport laboratory", "Лаборатория транспорта"},
            {"Debug build only", "Только debug-сборка"},
            {"Transport mode", "Режим транспорта"},
            {"Choose transport (debug only)", "Выбрать транспорт (только debug)"},
            {"UDP Lab: STUN through VPN", "UDP Lab: STUN через VPN"},
            {"Run debug UDP probe", "Проверить UDP в debug-режиме"},
            {"Import MBox profile", "Импортировать профиль MBox"},
            {"Import pasted link", "Импортировать вставленную ссылку"},
            {"Manual import", "Импорт вручную"},
            {"MBox settings", "Настройки MBox"},
            {"MBox subscriptions", "Подписки MBox"},
            {"Select MBox endpoint", "Выбор точки входа MBox"},
            {"Add MBox subscription", "Добавить подписку MBox"},
            {"Profile import failed", "Ошибка импорта профиля"},
            {"Confirm MBox profile", "Подтвердить профиль MBox"},
            {"Unsafe developer options", "Небезопасные параметры разработчика"},
            {"Remove subscription?", "Удалить подписку?"},
            {"MBox UDP laboratory", "Лаборатория UDP MBox"},
            {"Auto (recommended)", "Авто (рекомендуется)"},
            {"Cancel", "Отмена"}, {"Close", "Закрыть"}, {"OK", "ОК"},
            {"Import", "Импортировать"}, {"Review", "Проверить"},
            {"Remove", "Удалить"}, {"Add", "Добавить"},
            {"Replace profile", "Заменить профиль"}, {"Refresh", "Обновить"},
            {"Use subscription", "Выбрать подписку"},
            {"Choose endpoint", "Выбрать точку входа"},
            {"Refresh subscription", "Обновить подписку"},
            {"Remove locally", "Удалить локально"},
            {"Refresh all subscriptions", "Обновить все подписки"},
            {"Copy", "Копировать"}, {"Paste", "Вставить"},
            {"Clipboard is empty", "Буфер обмена пуст"},
            {"Import profile first", "Сначала импортируйте профиль"},
            {"Subscription saved", "Подписка сохранена"},
            {"Subscription selected", "Подписка выбрана"},
            {"Subscription updated", "Подписка обновлена"},
            {"Subscription update in progress", "Подписка обновляется"},
            {"Direct profile selected", "Прямой профиль выбран"},
            {"Disconnect VPN before changing profiles", "Отключите VPN перед сменой профиля"},
            {"Disconnect the VPN before switching transport", "Отключите VPN перед сменой транспорта"},
            {"No profile imported yet", "Профиль ещё не импортирован"},
        };
        for (String[] entry : entries) RU.put(entry[0], entry[1]);
    }

    static boolean russian(Context c) {
        String selected = c.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                .getString(KEY, "system");
        if ("ru".equals(selected)) return true;
        if ("en".equals(selected)) return false;
        Locale device = c.getResources().getConfiguration().getLocales().get(0);
        return "ru".equalsIgnoreCase(device.getLanguage());
    }

    static String selection(Context c) {
        return c.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                .getString(KEY, "system");
    }

    static boolean set(Context c, String code) {
        if (!"system".equals(code) && !"ru".equals(code) && !"en".equals(code))
            throw new IllegalArgumentException("Unsupported language");
        return c.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
                .edit().putString(KEY, code).commit();
    }

    static String text(Context c, String english) {
        if (!russian(c)) return english;
        String translated = RU.get(english);
        return translated == null ? english : translated;
    }

    static String text(Context c, String english, String russian) {
        return russian(c) ? russian : english;
    }

    private UiLanguage() {}
}
