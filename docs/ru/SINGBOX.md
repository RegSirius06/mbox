# Handoff в sing-box

<!-- MBOX-BETA4-CURRENT-DOCS -->
> **Версионный контекст:** ниже могут быть исторические наблюдения beta.2/beta.3. В **beta 4** работают прямой QUIC/HTTP3, UDP приложений и резервный HTTPS/Cronet. Актуальные сведения: [Android](ANDROID_BETA4.md), [транспорты](BETA_4_TRANSPORTS.md), [границы безопасности](SECURITY.md). Подписанный APK уже собран и установлен, но физический аудит утечек ещё не завершён.




[English](../SINGBOX.md)

По умолчанию MBox передаёт TCP streams в:

```text
127.0.0.1:2080
```

Рекомендуется sing-box `mixed` inbound. Уже sing-box решает: direct exit или Hysteria2/VLESS/SS/SOCKS/другой next hop. MBox сам не выбирает географию exit.
