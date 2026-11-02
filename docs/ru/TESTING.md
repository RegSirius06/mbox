# Тестирование

[English](../TESTING.md)

Автоматические gates: Go tests/race/vet, replay/order/full-duplex/flow-control, Noise vector/tamper, fuzz smoke, Python contract tests, source-layout guard.

Реальный Android 2026-11-02: Cronet+h2, Noise/tunnel, app traffic, stream cleanup, Wi-Fi/mobile, airplane-mode `WAIT_UNDERLYING`, server `SESSION SUPERSEDE`.

Это не заменяет тесты при активных белых списках, long soak, большой load и внешний security audit.
