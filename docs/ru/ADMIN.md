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
