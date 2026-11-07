# MBox Native QUIC — экспериментальный HTTP/3 транспорт (этап 1)

**Это лабораторный этап, не замена работающему REG.RU HTTPS, не Android APK и не автобалансировщик.**

Здесь QUIC — **реальный UDP на участке Arch → RU VPS**, а существующие HTTP-запросы MBox (включая зашифрованные Noise-записи) передаются через HTTP/3 **надёжными QUIC streams**. UDP приложений пока остаётся внутри надёжных MBox/HTTP/3 записей; QUIC DATAGRAM, Android `VpnService.protect()` и распределение отдельных SOCKS-потоков появятся после сквозного теста этого входа.

## Структура и безопасность

- `serve` — дополнительный QUIC/HTTP3 слушатель на VPS. Проверяет TLS и допустимые пути (`server.json`), ограничивает тело и число одновременных запросов, подписывает запросы общей с REG.RU HMAC-схемой. Пересылает только на **loopback** HTTP Origin.
- `client` — локальный `127.0.0.1:18451` HTTP-шлюз для отдельного экземпляра MBox core на Arch. Для удалённого соединения принимает **только HTTPS/3**, проверяет сертификат/CA, без HTTP/2 или HTTPS/TCP fallback.
- REG.RU relay и штатный MBox server продолжают работать независимо, ничего не перезаписываем.
- Модуль `common/originauth` переиспользуется старым relay и server и новым шлюзом. HMAC-ключ не нужно переносить на клиент.

## Сборка на Arch (Go 1.27.3)

```bash
cd ~/src/mbox-main/native-quic
go mod tidy
go test -race ./...
go build -trimpath -o ../dist/mbox-native-quic .
```

Go Modules изолированы в `native-quic/`, старые команды `GO111MODULE=off go test ./client ./server ./relay` сохраняются. `quic-go` закреплена на v0.65.0.

## VPS: отдельный порт UDP

Для начала убедиться, что `server.json` содержит фактический `transport.base_path` и пути `camouflage`; `mboxctl doctor` должен оставаться OK. При необходимости узнать путь origin key без раскрытия ключа:

```bash
jq -r '.transport.listen, .transport.base_path, .transport.origin_hmac_secret_file' /etc/mbox/server.json
```

Скопируйте бинарник `dist/mbox-native-quic` на VPS, например в `/usr/local/bin/mbox-native-quic`.

Пример временного сертификата для имени `quic.example.org` (заменить на своё доменное имя, которое разрешается в RU VPS):

```bash
install -d -m 700 /etc/mbox/quic
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes \
  -days 30 -keyout /etc/mbox/quic/server.key -out /etc/mbox/quic/server.crt \
  -subj '/CN=quic.example.org' -addext 'subjectAltName=DNS:quic.example.org'
chmod 600 /etc/mbox/quic/server.key
```

Только для лаборатории: на Arch скопировать **публичный сертификат** `server.crt` для `--ca` (не `server.key` и никогда не `origin-hmac.key`). В промышленной конфигурации предпочтительнее сертификат от доверенного CA.

Запустить на VPS от пользователя, который может читать `/etc/mbox/origin-hmac.key` и TLS private key:

```bash
/usr/local/bin/mbox-native-quic serve \
  --bind 0.0.0.0:7443 \
  --origin http://127.0.0.1:18449 \
  --server-config /etc/mbox/server.json \
  --secret-file /etc/mbox/origin-hmac.key \
  --cert /etc/mbox/quic/server.crt \
  --key /etc/mbox/quic/server.key
```

`--origin` должен точно совпадать с `transport.listen` из server.json, если оно отличается от примера. При открытии `7443/udp` на файрволе не изменяйте правило для HTTPS/REG.RU; на отдельном входе QUIC нет маскировки под общий хостинг.

## Arch: клиентский транспорт

```bash
cd ~/src/mbox-main
./dist/mbox-native-quic client \
  --bind 127.0.0.1:18451 \
  --remote https://quic.example.org:7443 \
  --ca ~/quic-server.crt \
  --paths '/BASE/api/v1/session,/BASE/api/v1/events/,/BASE/api/v1/sync/,/BASE/api/v1/session/'
```

`--paths` следует собрать по **реальным** `transport.base_path` и `camouflage` путям из `/etc/mbox/server.json` (не обязательно `/BASE/api/v1/...`); можно использовать `--server-config` с отдельным **обезличенным JSON**, содержащим только `transport.base_path` и `camouflage` (без приватных ключей) вместо `--paths`.

Для проверки end-to-end запустить **второй** экземпляр существующего MBox core с отдельным конфигом, в котором:

- `transport.url` заменён с внешнего REG.RU HTTPS URL на `http://127.0.0.1:18451/BASE`;
- `transport.listen` — другой свободный локальный SOCKS адрес, например `127.0.0.1:10891`;
- все ключи Noise/клиентские учётные данные остались прежними;
- `camouflage` и MBox session протоколы остались прежними.

Основной MBox остаётся подключённым через REG.RU в другом процессе. Не используйте основной Android TUN в этом этапе; **это не готовое переключение Android**. При открытии SOCKS через `10891` можно сравнить внешний IP с `10890` и посмотреть логи двух шлюзов. Следующий этап — выбор между ними по новым SOCKS-потокам, после тестов с реальным QUIC.

## Команды проверки

```bash
# На VPS (после запуска serve):
ss -lunp | grep ':7443'
# На Arch после запуска client:
ss -lntp | grep ':18451'
# Ошибка 404 здесь ожидаема: открывать произвольный /health через QUIC запрещено.
curl -i --max-time 5 http://127.0.0.1:18451/health
```

**Признак реального успеха** — второй MBox core установил Noise-сессию через `127.0.0.1:18451`, работал SOCKS TCP и прошёл UDP/STUN. Одних `ss` и `curl /health` недостаточно.

Перед публикацией прод-версии понадобятся: защищённый Android UDP-сокет, автоматическая оценка путей, автоматическое переключение **новых** потоков, QUIC DATAGRAM, нагрузочные тесты/проверка DNS/IPv6, автоматическое восстановление состояния. Ничего из этого в этапе 1 не заявлено реализованным.
