> **Панель beta.4.4:** предпочтительно только через SSH-туннель к приватному сокету Admin; не открывайте публичный порт без TLS/проверки доступа. В **«Подписки и точки входа» → «Конечные страны (выходы)»** имя **основного/default** — только надпись, сам маршрут origin SOCKS5 остаётся прежним для beta.4.3. Дополнительный выход задаётся ID (например, `kz`), названием и **локальным** SOCKS5 (например, `127.0.0.1:2085`). Сначала проверьте `curl --socks5-hostname ... https://ipinfo.io/country` прямо на origin. После сохранения `/etc/mbox/egress.json` должен читаться `mbox` (`root:mbox 0640`). Обновите подписку в Android — одна URL выдаст DK/KZ для beta.4.4, v1 у старого клиента останется default. Точки входа HTTPS и конечные страны — **разные формы**. Не нужно менять URL/PSK для добавления страны. [Подписки](MBOX_SUBSCRIPTIONS.md).

<!-- MBOX_BETA43_DOCS10 -->
> **beta.4.3 context:** Актуальная инструкция beta.4.3: [установка сервера](SERVER_QUICKSTART.md), [подготовка релиза](RELEASE_ENGINEERING_BETA43.md) и [публичный домен админки](MBOX_ADMIN_PUBLIC_ACCESS.md). Ниже могут быть команды для прежних версий.

# Администрирование

[English](../ADMIN.md)

Основные команды:

```bash
mboxctl doctor
mboxctl resources
mboxctl user list
mboxctl user show USER
mboxctl user link USER
mboxctl backup list
mboxctl cover status
```

Дефолт `max_streams_per_session=128` откалиброван реальным Android stress test. Global limits сохраняются даже для `--unlimited`.

REG.RU frontend живёт на shared hosting, не на origin VPS. См. [REG_RU_SHARED_HOSTING.md](REG_RU_SHARED_HOSTING.md).

При revoke/disable/expiry/PSK rotation активные сессии могут быть закрыты сразу. Same-device reconnect после жёсткого обрыва использует session supersession.
