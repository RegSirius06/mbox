# Handoff в sing-box

[English](../SINGBOX.md)

По умолчанию MBox передаёт TCP streams в:

```text
127.0.0.1:2080
```

Рекомендуется sing-box `mixed` inbound. Уже sing-box решает: direct exit или Hysteria2/VLESS/SS/SOCKS/другой next hop. MBox сам не выбирает географию exit.
