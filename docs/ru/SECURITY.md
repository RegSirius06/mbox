# Безопасность

[English](../SECURITY.md)

MBox экспериментальный и не проходил независимый security audit.

- сессии используют `Noise_NKpsk0_25519_AESGCM_SHA256`;
- legacy crypto downgrade отсутствует;
- exact retry переиспользует исходный ciphertext, а не nonce;
- pre-auth OPEN ограничен по concurrent/rate;
- static server key загружается один раз и проверяются права;
- counter exhaustion fail-closed;
- Android `device_id` находится внутри аутентифицированного Noise OPEN;
- новая сессия supersede'ит stale-сессию только того же устройства;
- новые HTTPS provisioning links кладут секрет во fragment, а не query.

Секреты: `/etc/mbox/server.key`, users DB, backups, `mbox://` links. Не коммитьте их в Git.

Cronet уменьшает fingerprint Go TLS/HTTP, но не является security boundary и не гарантирует невидимость для DPI.
