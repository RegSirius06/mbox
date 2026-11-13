# MBox v1.0.0-beta.4.1 — Android prerelease

**Android 10+ / ARM64 / `io.mbox.android` / versionCode 8.** Исправляющее обновление надёжности QUIC и HTTPS по сравнению с beta.4.

**Изменения:** аутентифицированный Noise heartbeat; контроль QUIC gateway и HEV по отдельности; восстановление с сохранением TUN; понятные CONNECTING / VERIFYING / RECOVERING; расширенный native QUIC CI. Wire protocol, старые HTTPS-only профили и серверный API не изменены. Ради heartbeat сервер обновлять не нужно.

**Проверки:** разработчик сообщил о прохождении 5 основных сценариев подключения и восстановления; прошли Go/native QUIC race-тесты и собран debug APK. Физический IPv4/IPv6/DNS/UDP packet capture, полная матрица Lockdown, принудительный отказ gateway, длительный soak и приёмка именно release-подписанного APK ещё не завершены. **Это не независимо аудированный VPN с доказанным отсутствием утечек и гарантированным обходом DPI.**

**Ограничения:** Auto может обрывать действующие потоки; QUIC DATAGRAM и multipath нет; UDP приложений через HTTPS идёт поверх TCP. Старый тестовый QUIC-сертификат истекает 7 декабря 2026 года; ротация CA/leaf **не развёрнута**.

- **SHA-256 APK:** `PENDING_FINAL_SIGNED_APK_HASH`
- **SHA-256 сертификата подписи:** `PENDING_VERIFIED_EXISTING_SIGNER_PIN`
- **Коммит:** `PENDING_FINAL_GIT_SHA`

[Русские release notes](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4.1/RELEASE_NOTES.ru.md) · [English release notes](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4.1/RELEASE_NOTES.md).

**Не публиковать этот черновик с PENDING.** Прикладывать только production-signed APK с прежним ключом и checksum, не debug/unsigned, секретные профили или сырые логи.
