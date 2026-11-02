# Server cover traffic

[English](../COVER.md)

Cover traffic — опциональная серверная активность через публичный frontend, когда реальных MBox-сессий нет. Она выключена по умолчанию и подавляется при активных сессиях.

```bash
mboxctl cover simulate --hours 24
mboxctl cover test
mboxctl cover enable
mboxctl cover status
```

Cover меняет историю запросов frontend, но не скрывает отсутствие трафика на конкретном мобильном last-mile.
