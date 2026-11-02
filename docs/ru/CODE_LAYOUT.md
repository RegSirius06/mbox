# Структура кода

[English](../CODE_LAYOUT.md)

`client/main.go` и `server/main.go` — bootstrap, а логика разнесена по `session`, `http_transport`, `uplink/downlink`, `streams`, `camouflage`, `network`, `handshake`, `maintenance` и т.д.

На первом этапе это остаётся один Go package на client/server: физическая модульность без рискованного package/API refactor. CI ограничивает размер исходников, чтобы монолит не вырос обратно.
