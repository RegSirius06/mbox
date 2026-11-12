# MBox

Русский | [English](README.md)

<!-- BETA4-DOCS-START -->
## Разработка beta 4 — UDP и нативный QUIC (ещё не опубликована)

В разрабатываемой beta 4 UDP приложений передаётся через существующий **HTTPS/TCP + Cronet + FastCGI**, а также появился отдельный **прямой native QUIC/HTTP3 поверх UDP**. Android **Auto** выбирает QUIC при доступности и переключается на HTTPS при отказе. Это переключение между соединениями, **не multipath**. Опубликованная beta 3 остаётся предыдущим TCP-only релизом.

- [Точная схема двух транспортов и ограничения](docs/ru/BETA_4_TRANSPORTS.md) ([EN](docs/BETA_4_TRANSPORTS.md))
- [Полевые тесты, сбой FastCGI и баг-репорты](docs/ru/BETA_4_FIELD_TESTING.md) ([EN](docs/BETA_4_FIELD_TESTING.md))
- [Заметки beta 4 — черновик](docs/ru/BETA_4_RELEASE_NOTES.md) ([EN](docs/BETA_4_RELEASE_NOTES.md))
- [Планы Linux/Windows/macOS](docs/ru/PLATFORM_ROADMAP.md) ([EN](docs/PLATFORM_ROADMAP.md))
- [Как сообщить об ошибке или прислать PR](CONTRIBUTING.ru.md) ([EN](CONTRIBUTING.md))

**Проверка debug APK не означает готовность beta 4:** native QUIC должен попасть в подписанную release-сборку; отсутствие физических утечек и долговременная стабильность пока не доказаны независимыми испытаниями.
<!-- BETA4-DOCS-END -->


> **Текущая экспериментальная версия: `1.0.0-beta.3`.** Публичный Android-клиент beta.2.1 несовместим с origin при обязательном `cmac`. Noise-record и PSK/профили прежние. 05.11.2026 новая версия работала на одном Android при активных мобильных белых списках, но отсутствие сетевых утечек и длительная стабильность ещё не доказаны. См. [release notes](RELEASE_NOTES.ru.md) и [статус приёмки](docs/BETA_3_RELEASE_STATUS.ru.md).

MBox — экспериментальный шифрованный мультиплексированный транспорт пользовательских TCP-потоков, а в разрабатываемой beta 4 также UDP. Режим HTTPS работает через веб-посредников с буферизацией законченных HTTP-запросов/ответов; прямой native QUIC — отдельный режим.

Типичная схема:

```text
Android
  -> VpnService/TUN
  -> HEV -> MBox SOCKS
  -> Chromium Cronet HTTPS
  -> REG.RU shared hosting + FastCGI relay
  -> MBox origin VPS
  -> sing-box 127.0.0.1:2080
  -> direct / Hysteria2 / VLESS / другой exit
```

## Что изменилось после beta.2

### Внешний fingerprint Android

В beta.2 внешний HTTPS на Android делал Go `crypto/tls + net/http`. Это давало довольно очевидный implementation fingerprint. В текущей ветке внешний HTTPS по умолчанию делает **bundled Chromium Cronet** через локальный аутентифицированный bridge, а Go оставляет у себя Noise/MUX/replay/ACK/flow-control.

На реальном Android 2026-11-02 подтверждены:

```text
stack=cronet
HTTP/2.0
alpn=h2
MBOX_OPEN_OK
NOISE_OK
TUNNEL_ESTABLISHED
```

Telegram, Instagram и видео через туннель работали. Для внешнего провайдера это уже не характерный Go TLS/HTTP стек, а цельный Chromium-derived application network stack. Это **существенно менее очевидный fingerprint**, но не заявление «мы неотличимы от Chrome» и не гарантия обхода любого DPI. Во внешнем HTTPS-стеке Cronet используется HTTP/2; отдельный native QUIC/HTTP3 добавлен в ветке beta 4.

### Android lifecycle / метро / handover

Закрыты реальные баги, найденные на телефоне:

- собственный VPN больше не принимается за смену физической сети;
- мониторятся только `INTERNET + NOT_VPN` underlying networks;
- при полном исчезновении связи клиент пишет `WAIT_UNDERLYING` и не накручивает backoff бесполезными попытками;
- при возврате Wi-Fi/LTE reconnect запускается сразу;
- airplane-mode / «метро» протестирован;
- стабильный случайный `device_id` передаётся внутри аутентифицированного Noise OPEN; новая сессия заменяет stale-сессию **только того же устройства**;
- серверные stale-сессии больше не забивают лимит 4 и не приводят к 503 после нескольких жёстких обрывов.

### Ресурсы

При агрессивном переключении Telegram/Instagram/YouTube реальный пик составил 64 одновременных stream. После idle число потоков падало, leak не подтвердился. Поэтому дефолт:

```text
max_streams_per_session = 128
max_total_streams = 1024
```

## Безопасность

- только `Noise_NKpsk0_25519_AESGCM_SHA256`; legacy crypto fallback отсутствует;
- exact ciphertext replay для потерянного DOWN response;
- bounded queues/flow control/quotas;
- pre-auth OPEN rate/concurrency limits;
- server static key читается один раз при старте и проверяются права;
- provisioning secret в новых HTTPS App Links находится во fragment (`#profile=...`), а не в query;
- `device_id` находится внутри Noise-аутентифицированного OPEN payload.

Проект всё ещё не проходил независимый security audit.

## REG.RU: важная часть схемы

Для проверенной схемы нужен именно **виртуальный/shared hosting REG.RU**, а не VPS REG.RU. FastCGI relay и обычный сайт живут на shared hosting, а MBox origin может быть вообще в другом месте.

Для нового развёртывания разумно брать тариф из скоростной линейки (`Хост-0/Хост-1/Хост-3` по текущей документации REG.RU), а не самый дешёвый тариф для простых статических страниц. REG.RU заявляет безлимитный трафик на shared-hosting тарифах, но это не означает отсутствие fair-use/нагрузочных/договорных ограничений.

**Важно:** актуальные материалы REG.RU содержат ограничения на proxy-сервисы на shared hosting. Перед использованием нужно проверить действующие условия и убедиться, что конкретный сценарий разрешён провайдером. Техническая работоспособность MBox не отменяет правила хостинга.

Подробно: [гайд по REG.RU shared hosting](docs/ru/REG_RU_SHARED_HOSTING.md).

## Белые списки и операторы

В ходе испытаний разработки beta 4 **HTTPS/Cronet и Auto работали при активных мобильных ограничениях на устройстве разработчика**; универсальная совместимость не доказана. Принципиальная схема frontend/FastCGI и Noise-wire не поменялась, поэтому на сетях, где предыдущая версия работала, регрессия сейчас не ожидается — но это инженерное ожидание, а не свежий тест.

На 13.11.2026 разработчик сообщил об одном успешном тесте на МегаФоне; это не доказательство работы у всех абонентов. Ранее `.fun` frontend мог не открываться на MegaFon даже в обычном браузере, то есть проблема была до MBox. На MTS/T2 такой frontend в ограниченном режиме был доступен. Следующий тест — `.fun` и `.ru` на одном и том же REG.RU shared-host backend. `.ru` — гипотеза о другой обработке allow-list/SNI/category, а не обещание.

С 1 сентября 2026 администраторы `.ru/.рф/.su` должны проходить предусмотренную правилами идентификацию через ЕСИА; у хостинга также есть процедура идентификации владельца услуги.

## Быстрая установка сервера

На Debian/Ubuntu-подобном VPS:

```bash
chmod +x install.sh
sudo ./install.sh
```

Проверка:

```bash
sudo mboxctl doctor
sudo mboxctl resources
systemctl status mbox-server --no-pager
```

Создание пользователя:

```bash
sudo mboxctl user add android-main
sudo mboxctl user link android-main
```

## Android build на Arch

```bash
sudo pacman -S --needed jdk17-openjdk
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
hash -r

./android/tools/build-debug.sh
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

Для A/B диагностики:

```bash
sudo mboxctl user link USER --android-http-stack cronet --network-debug
sudo mboxctl user link USER --android-http-stack go --network-debug
```

## REG.RU relay

```bash
sudo mboxctl relay bundle
sudo mboxctl relay paths
```

SSH/SCP:

```bash
sudo mboxctl relay deploy \
  --ssh USER@HOST \
  --remote-dir /var/www/USER/data/www/front.example.com
```

FTPS:

```bash
sudo mboxctl relay deploy \
  --ftp-host HOST \
  --ftp-user USER \
  --remote-dir /path/to/webroot
```

Проверка frontend:

```bash
sudo mboxctl relay test
```

## Диагностика сети

MBox логирует стадии:

```text
DNS -> TCP -> TLS -> certificate -> ALPN -> HTTP -> MBOX_OPEN -> NOISE -> TUNNEL
```

Credential-free probe:

```bash
GO111MODULE=off go build -o /tmp/mbox-netprobe ./tools/mbox-netprobe
/tmp/mbox-netprobe -json https://HOST/
```

Android:

```bash
adb logcat -c
adb logcat -v threadtime -s MBox:I AndroidRuntime:E
```

## Документация на русском

- [Индекс русских гайдов](docs/ru/README.md)
- [Архитектура](docs/ru/ARCHITECTURE.md)
- [Администрирование](docs/ru/ADMIN.md)
- [REG.RU shared hosting](docs/ru/REG_RU_SHARED_HOSTING.md)
- [Совместимость сетей](docs/ru/NETWORK_COMPATIBILITY.md)
- [Операторский тест `.fun`/`.ru`](docs/ru/OPERATOR_TEST.md)
- [Fingerprint/Cronet](docs/ru/FINGERPRINT_BASELINE.md)
- [Диагностика](docs/ru/NETWORK_DIAGNOSTICS.md)
- [Безопасность](docs/ru/SECURITY.md)
- [Ресурсы](docs/ru/RESOURCES.md)
- [Тестирование](docs/ru/TESTING.md)

Полный англоязычный README сохраняется в [`README.md`](README.md).
