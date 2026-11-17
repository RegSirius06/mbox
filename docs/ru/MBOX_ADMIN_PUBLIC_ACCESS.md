# MBox Admin: SSH и отдельный HTTPS-домен

**По умолчанию:** только Unix-сокет `/run/mbox-admin/admin.sock` (каталог `0700`, сокет `0600`), рекомендуемый доступ через SSH. Публичный режим **не включается** при применении патча, `install.sh` его не включает, порты автоматически не открываются.

**Дополнительный режим:** `https://panel.example.com` с Caddy (TLS), отдельной Basic Auth и обычным входом MBox Admin. Бэкенд по-прежнему **не слушает TCP**, Caddy подключается к Unix-сокету. Сервер разрешает `Host` только для локального SSH или точно указанного домена; у публичных POST обязателен правильный `Origin`, cookie получает `Secure; HttpOnly; SameSite=Strict`. Заголовкам `X-Forwarded-*` нельзя расширить доступ.

## Подготовка и включение (только по явному решению администратора)

1. Установить новую версию `tools/mbox-admin/mbox_admin.py` и `index.html` **отдельно от origin**. Сохранить резервную копию существующих файлов. Изменения Python требуют `systemctl restart mbox-admin.service`; перезапускать `mbox-server`, QUIC и REG.RU не нужно.
2. Убедиться, что `getent passwd caddy` и `getent group caddy` успешны. Нужно, чтобы Caddy действительно работал под пользователем/группой `caddy`. Использование других пользователей требует соответствующей корректировки drop-in.
3. Настроить DNS `panel.example.com` на VPS. Убедиться, что Caddy может выпустить TLS-сертификат и что порт 443 обслуживается существующим Caddy. **Не открывать порт Python-админки.**
4. Создать `/etc/systemd/system/mbox-admin.service.d/public.conf` по примеру `deploy/systemd/mbox-admin-public.conf.example`, заменить домен. Существующий `/etc/mbox-admin/admin.env` **не перезаписывать**.
5. `systemctl daemon-reload && systemctl restart mbox-admin.service`. Проверить `systemctl is-active mbox-admin` и `stat -c '%a %U:%G %n' /run/mbox-admin /run/mbox-admin/admin.sock` — ожидается `750 root:caddy` и `660 root:caddy`. Если это не так, не открывать домен.
6. Сгенерировать отдельный пароль для Caddy (`caddy hash-password`) и вписать хэш в `deploy/caddy/mbox-admin-public.caddy.example`, заменить обе записи домена. **Не использовать пароль MBox Admin повторно.** Добавить site block в существующий Caddyfile, не перезаписывая работающие маршруты MBox. Для Caddy 2.7 и старше директива называется `basicauth`, а не `basic_auth`.
7. Сделать резервную копию Caddyfile. Выполнить `caddy validate --config /etc/caddy/Caddyfile` и только после успеха `caddy reload --config /etc/caddy/Caddyfile`. Это обновит Caddy-конфигурацию, потенциально затрагивающую активные соединения; делать отдельно от деплоя origin.
8. Проверить HTTPS/сертификат, два последовательных входа (Basic Auth + MBox), создание CSRF-токена, запрет чужого `Origin`, `Secure` на session cookie, сохранение SSH-туннеля. Для каждого теста не менять пользователей или настройки production.

### Откат публичного режима

Удалить **только** `public.conf` drop-in, убрать отдельный site block панели из Caddyfile (не другие маршруты), проверить Caddyfile, reload Caddy; затем `systemctl daemon-reload && systemctl restart mbox-admin.service`. Права Unix-сокета вернутся к `0700/0600`. SSH останется доступен.

### Важные границы безопасности

- HTTPS и Basic Auth **обязательны**, пароль MBox Admin остаётся второй ступенью; по возможности дополнительно ограничить IP или использовать приватную сеть.
- Публичный административный интерфейс запускает операции с привилегиями root: он значительно чувствительнее обычного сайта. Не включать на широкую аудиторию.
- Не использовать CDN с неожиданным HTTP-доступом к origin; не полагаться на `X-Forwarded-Proto` при выборе `Secure` cookie.
- Никогда не отправлять PSK, подписки, пароли и содержимое `/etc/mbox-admin` в тикеты и переписку.
- Изменение `Host` в панели не является настройкой сервера: нужен отдельный opt-in системного окружения и Caddy.
