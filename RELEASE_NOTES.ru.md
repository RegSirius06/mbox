# MBox 1.0.0-beta.2.2 — кандидат в разработке (только исходники)

Это **не** релиз с подтверждённым выполнением полного security ТЗ. Относительно `68f624c` ограничена параллельная обработка origin HTTP, добавлена проверка заголовков HMAC до чтения body, оптимизирован TTL sweep nonce replay, ужесточён разбор управляющей HTTP query, проверяется пин сертификата подписанного APK и согласованы версия Android `versionCode=5`, `beta.2.2`. Noise record / PSK не изменены. APK здесь **не собирался и не подписывался**, production **не обновлялся**.

Постатейная матрица ТЗ и неустранённые риски: `docs/BETA_2_2_HARDENING_MATRIX_RU.md`. Нет полной приёмки Android Lockdown / физических утечек, нативного UDP, одноразового подписанного provisioning, multi-day soak и fingerprint lab.

## Предыдущие заметки beta.2.1

# Release notes — текущий development snapshot

[English](RELEASE_NOTES.md)

`1.0.0-beta.2.1` — совместимый с beta.2 hardening-релиз. Главное изменение — внешний Android HTTPS теперь делает Chromium Cronet, поэтому наружу не торчит характерный Go TLS/HTTP fingerprint. `v1.0.0-beta.2` остаётся предыдущим baseline. На реальном телефоне подтверждены Cronet+h2+Noise+tunnel, handover, airplane-mode recovery и session supersession.

Это не точная эмуляция Chrome и не гарантия обхода любого DPI. В активных БС текущая ветка ещё не тестировалась. MegaFon неизвестен; `.ru` — гипотеза для контролируемого сравнения с `.fun`.
