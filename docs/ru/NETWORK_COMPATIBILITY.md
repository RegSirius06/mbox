# Совместимость сетей

[English](../NETWORK_COMPATIBILITY.md)

Историческое наблюдение до текущей Cronet-ветки: `.fun` shared-host frontend был доступен в ограниченном режиме на MTS/T2, а на MegaFon мог не открываться даже в обычном браузере. Это указывает, что MegaFon может ломаться раньше MBox/Noise.

Текущая ветка ещё не тестировалась при активных БС. Публичная shared-host/FastCGI схема и beta.2 wire сохранены, поэтому на ранее работавших сетях регрессия не ожидается, но это нужно подтвердить. MegaFon — неизвестно.

Следующий контролируемый эксперимент: `.fun` и `.ru` на одном backend/IP/TLS. `.ru` — гипотеза, не гарантированный обход.

При сбое собираем:

```text
DNS -> IP -> TCP -> TLS/cert -> ALPN -> HTTP -> OPEN -> Noise -> tunnel
```

См. [OPERATOR_TEST.md](OPERATOR_TEST.md) и [NETWORK_DIAGNOSTICS.md](NETWORK_DIAGNOSTICS.md).
