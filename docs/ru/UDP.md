# UDP в MBox beta 4

[English](../UDP.md) · [Транспортная матрица](BETA_4_TRANSPORTS.md) · [Тест защиты](../ANDROID_FAIL_CLOSED_TEST_PLAN.md)

**В beta 4 UDP приложений поддерживается** и переносится внутри аутентифицированных и зашифрованных MBox-записей. Флаг `enable_udp` включает штатную работу независимо от developer mode. В beta.2 существовал временный запрет SOCKS UDP ASSOCIATE до реализации UDP; **этот старый запрет не описывает beta 4**.

- HTTPS/Cronet: пользовательские UDP-датаграммы внутри MBox поверх HTTPS/TCP (наружу идёт TCP).
- QUIC/HTTP3: снаружи реальный UDP к gateway на VPS; внутри HTTP/3 **streams**, не QUIC DATAGRAM.
- Прямой выход UDP приложений в физическую сеть мимо MBox не является предусмотренным fallback.

Для доказательства отсутствия утечек всё ещё необходимы packet-capture проверки реального Android на Wi-Fi/LTE: TUN, DNS, IPv4/IPv6, аварии и переключения сети. Используйте Always-on и Lockdown. Не отключайте TLS-проверку при истечении QUIC-сертификата.
