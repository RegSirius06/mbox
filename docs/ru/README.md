# Документация MBox — 1.0.0-beta.4.4

[English](../README.md) · [Главная](../../README.ru.md)

**Актуальные материалы** (старые страницы статуса/планов beta.4.x — история, не описание текущих возможностей):

1. **Пользователь:** [Установка Android](GETTING_STARTED.md) · [Профили и подписки](ANDROID_MULTI_SUBSCRIPTIONS.md) · [Примечания](../../RELEASE_NOTES.ru.md).
2. **Администратор:** [Первая установка](SERVER_QUICKSTART.md) · [Панель](ADMIN.md) · [Подписки и выходы](MBOX_SUBSCRIPTIONS.md) · [Риски REG.RU FastCGI](REG_RU_SHARED_HOSTING.md) · [Эксплуатация](BETA44_OPERATIONS.md).
3. **Архитектура:** [Все маршруты](ARCHITECTURE.md) · [DATAGRAM по требованию](BETA44_DATAGRAM_EXPERIMENT.md) · [Безопасность](SECURITY.md) · [Совместимость сетей](NETWORK_COMPATIBILITY.md).
4. **Выпуск:** [Чек-лист](../../RELEASE_CHECKLIST.ru.md) · [Тестирование](TESTING.md) · [Описание релиза](GITHUB_RELEASE_BODY.md) · [GitHub Issues](https://github.com/RegSirius06/mbox/issues).

**Совместимость:** подписки v1 (beta.4.3) и v2 (beta.4.4, страны) отличаются от payload Noise OPEN MBD1/MBD2 и версии record 2. Маршрут по умолчанию сохранён намеренно для старого клиента; неподдерживаемые и небезопасные прежние версии не гарантированы.

**Открытые риски:** CPU/воркеры REG.RU, батарея/долгий простой Android, реальные физические утечки/Lockdown и проверка уведомления после публикации. Зелёные unit-тесты не закрывают их.
