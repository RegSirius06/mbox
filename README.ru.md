# MBox

Русский | [English](README.md)

> **Текущая версия:** `1.0.0-beta.2.1`. Это совместимый с beta.2 hardening-релиз: Noise-wire и существующие credentials beta.2 сохраняются, а поверх них добавлены P0/P1 диагностика сети, Chromium Cronet для внешнего HTTPS на Android, dual-stack/handover, supersession сессий одного устройства и модульная раскладка кода. `v1.0.0-beta.2` остаётся предыдущим тегированным baseline. beta.2.1 **ещё не проходила свежие полевые испытания в режиме мобильных белых списков**.

MBox — экспериментальный шифрованный мультиплексированный TCP-транспорт поверх коротких законченных HTTP(S)-запросов. Он рассчитан на промежуточные веб-хосты, которые буферизуют запрос/ответ целиком и не дают обычный долгоживущий streaming tunnel.

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

Telegram, Instagram и видео через туннель работали. Для внешнего провайдера это уже не характерный Go TLS/HTTP стек, а цельный Chromium-derived application network stack. Это **существенно менее очевидный fingerprint**, но не заявление «мы неотличимы от Chrome» и не гарантия обхода любого DPI. QUIC пока намеренно выключен, HTTP/2 включён.

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

Текущая Cronet-ветка **ещё не тестировалась в поле во время активных БС**. Принципиальная схема frontend/FastCGI и Noise-wire не поменялась, поэтому на сетях, где предыдущая версия работала, регрессия сейчас не ожидается — но это инженерное ожидание, а не свежий тест.

MegaFon остаётся неизвестным. Ранее `.fun` frontend мог не открываться на MegaFon даже в обычном браузере, то есть проблема была до MBox. На MTS/T2 такой frontend в ограниченном режиме был доступен. Следующий тест — `.fun` и `.ru` на одном и том же REG.RU shared-host backend. `.ru` — гипотеза о другой обработке allow-list/SNI/category, а не обещание.

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
