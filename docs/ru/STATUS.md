# Статус MBox

[English](../STATUS.md)

`1.0.0-beta.2.1` — текущая версия/кандидат на тег. Это совместимый с beta.2 hardening-релиз: Noise-wire и credentials beta.2 сохранены, добавлены Cronet, диагностика, dual-stack/handover, wait-for-network, same-device session supersession, pre-auth limits и модульность кода. `v1.0.0-beta.2` остаётся предыдущим тегированным baseline.

На реальном Android подтверждены Cronet+h2, Noise/tunnel, Telegram/Instagram/video, Wi-Fi/mobile handover и airplane-mode recovery.

Ещё не закрыто:

- текущая ветка в активных БС MTS/T2/MegaFon;
- `.fun` vs `.ru` на одном backend;
- Chrome vs Cronet packet fingerprint;
- signed provisioning/trust anchor;
- Always-on/lockdown;
- длинный soak/load и внешний security audit.
