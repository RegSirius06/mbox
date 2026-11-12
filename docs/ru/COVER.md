# Server cover traffic

<!-- MBOX-BETA4-CURRENT-DOCS -->
> **Версионный контекст:** ниже могут быть исторические наблюдения beta.2/beta.3. В **beta 4** работают прямой QUIC/HTTP3, UDP приложений и резервный HTTPS/Cronet. Актуальные сведения: [Android](ANDROID_BETA4.md), [транспорты](BETA_4_TRANSPORTS.md), [границы безопасности](SECURITY.md). Подписанный APK уже собран и установлен, но физический аудит утечек ещё не завершён.


[English](../COVER.md)

Cover traffic — опциональная серверная активность через публичный frontend, когда реальных MBox-сессий нет. Она выключена по умолчанию и подавляется при активных сессиях.

```bash
mboxctl cover simulate --hours 24
mboxctl cover test
mboxctl cover enable
mboxctl cover status
```

Cover меняет историю запросов frontend, но не скрывает отсутствие трафика на конкретном мобильном last-mile.
