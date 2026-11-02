# Полевой тест операторов: `.fun` vs `.ru`

[English](../OPERATOR_TEST.md)

Цель — определить слой сбоя, а не гадать про DPI.

Для Wi-Fi/MTS/T2/MegaFon на одном телефоне проверяем:

1. Chrome `.fun`;
2. Chrome `.ru`;
3. MBox Cronet `.fun`;
4. MBox Cronet `.ru`;
5. MBox Go diagnostic `.fun`;
6. MBox Go diagnostic `.ru`;
7. `mbox-netprobe`.

Два hostname должны вести на максимально одинаковый shared-host backend.

```bash
GO111MODULE=off go build -o /tmp/mbox-netprobe ./tools/mbox-netprobe
/tmp/mbox-netprobe -json https://FUN_HOST/ https://RU_HOST/ | tee probe.jsonl
```

Интерпретация:

- TCP не идёт у обоих -> IP/subnet/routing/allow-list;
- TCP есть, TLS нет -> SNI/ClientHello/cert/TLS policy;
- TLS есть, HTTP нет -> frontend/HTTP filtering;
- Chrome работает, Go-MBox нет, Cronet-MBox работает -> fingerprint стека важен;
- `.ru` работает, `.fun` нет на одном backend -> сильная гипотеза hostname/SNI/category;
- HTTP доходит, OPEN/Noise падает -> проблема внутри MBox.

Один SIM/регион/вечер не доказывает правило для всего оператора.
