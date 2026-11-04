# Статус MBox

[English](../STATUS.md)

## Текущая версия: `1.0.0-beta.3` (экспериментальный prerelease, 05.11.2026)

- Проверена одна цепочка Android → REG.RU FastCGI → root-managed RU VPS. Во время действующих ограничений мобильного Интернета соединение и Telegram работали.
- Публичная beta.2.1 не отправляет `cmac`, поэтому несовместима с origin при `auth.require_authenticated_control=true`. Noise/PSK формат сохранён; клиент beta.2.2 с `cmac` совместим.
- Cronet HTTPS/H2, TCP через Noise, полный IPv4/IPv6 TUN; UDP ASSOCIATE/BIND блокируются, нативного UDP нет.
- Ещё не доказано packet capture отсутствие DNS/TCP/UDP/IPv6 утечек; не закончены crash/Lockdown, soak, нагрузочные тесты REG.RU, signed enrollment, сравнение сетевого поведения с Chrome.
- В публичный GitHub release не выкладывать debug APK. Только подписанный release-ключом APK с проверенным SHA-256 сертификата и checksum.

См. `RELEASE_NOTES.ru.md`, `docs/BETA_3_RELEASE_STATUS.ru.md` и план сетевых испытаний.

---

## Исторический статус beta.2.1 (02.11.2026)

# Статус MBox

[English](../STATUS.md)

`1.0.0-beta.2.1` — историческая версия/кандидат на тег. Это совместимый с beta.2 hardening-релиз: Noise-wire и credentials beta.2 сохранены, добавлены Cronet, диагностика, dual-stack/handover, wait-for-network, same-device session supersession, pre-auth limits и модульность кода. `v1.0.0-beta.2` остаётся предыдущим тегированным baseline.

На реальном Android подтверждены Cronet+h2, Noise/tunnel, Telegram/Instagram/video, Wi-Fi/mobile handover и airplane-mode recovery.

Ещё не закрыто:

- текущая ветка в активных БС MTS/T2/MegaFon;
- `.fun` vs `.ru` на одном backend;
- Chrome vs Cronet packet fingerprint;
- signed provisioning/trust anchor;
- Always-on/lockdown;
- длинный soak/load и внешний security audit.
