# MBox — открытые требования hardening / release gates (4 ноября 2026)

**База:** исходники до следующего патча, коммит `71441ca`, с локальным патчем
`android-bridge-preauth + credential-admin-hardening` в этом документе.
**Контур:** Android → REG.RU shared hosting/FastCGI → MBox origin → sing-box → exit.
**Область оценки:** исходники и автоматические тесты; живой origin/REG.RU/телефон
в этой проверке не изменялись. «Есть код» ≠ «прошло эксплуатационную приёмку».

Обозначения: **НЕТ** — отсутствует требуемая реализация; **ЧАСТИЧНО** —
есть реализация, но не весь контракт/сценарий; **НЕ ДОКАЗАНО** — код/настройка
есть, однако отсутствуют обязательные внешние измерения; **НЕ ПРОВЕДЕНО** —
нужен запуск/отчёт испытания; **ИСПРАВЛЕНО В ПАТЧЕ** — изменён код и добавлены
тесты, Android на устройстве ещё не проверялся.

## P0 — безопасность/корректность

| № | Требование | Статус | Основание/критерий закрытия |
|---|---|---|---|
| 1 | Отсутствие обхода MBox при нормальном Android VPN | НЕ ДОКАЗАНО | Полный физический Wi-Fi capture, сопоставленный с TUN: IPv4/IPv6/TCP/UDP/DNS/WebRTC/QUIC; `docs/ANDROID_FAIL_CLOSED_TEST_PLAN.md` |
| 2 | Отсутствие обхода при потере origin, краше Go, `force-stop`, Wi-Fi/LTE | НЕ ДОКАЗАНО | Capture с Lockdown, в том числе LTE отдельно; ADB/logcat недостаточно |
| 3 | End-to-end native UDP без утечки в физическую сеть | НЕТ | SOCKS `UDP ASSOCIATE` намеренно отвергается; `docs/UDP.md`; нужна UDP-архитектура на обоих концах и анализ TUN |
| 4 | Android Always-on/Lockdown на разных прошивках и после аварий | ЧАСТИЧНО | Реальное Xiaomi автозапускается при reboot, network handover прошёл; `am crash` не восстановился; нужна матрица устройств, OS restart/crash/force-stop и capture |
| 5 | Подтверждение исходящего `Cronet.bindToNetwork` и `VpnService.protect` без утечки | НЕ ДОКАЗАНО | Сетевой capture до маршрутизации и разбор фактических физических сокетов; приложение не должно исключаться целиком из VPN |
| 6 | Безопасность DOWN request metadata (`seq`, `ack`) при скомпрометированном frontend | ЧАСТИЧНО | `seq/ack` передаются в URL и не аутентифицированы Noise; origin HMAC доверяет relay. Требуется определённая threat model и протокольная аутентификация ACK/метаданных, если relay недоверенный |
| 7 | Надёжность DOWN в реальных 502/504/lost response после commit | ЧАСТИЧНО | Есть exact ciphertext replay и тесты; нужен сетевой chaos/cut-after-commit через FastCGI/frontend с byte-for-byte сверкой |
| 8 | Полные квоты на HTTP/FCGI/админ-сокеты, RSS/FD/goroutines, медленные соединения | ЧАСТИЧНО | Есть лимиты MUX/queues/handshake; RSS HTTP/FCGI/Android ещё не измерялся на враждебных нагрузках |
| 9 | Android Cronet loopback: bounded unauthenticated work | ИСПРАВЛЕНО В ПАТЧЕ | До патча 8 workers могли дочитывать 8 MiB + заголовки *до* проверки token; теперь ранний token-gate + JDK test, нужна APK проверка |
| 10 | Android импорт профиля: ограничения памяти на недоверенный VIEW/clipboard | ИСПРАВЛЕНО В ПАТЧЕ | Жёсткий limit 64 Ki-char на link и 48 KiB JSON; нужны инструментальные Android intent fuzz/stress |
| 11 | Админ-сокет: bounded goroutines и безопасное управление pathname | ИСПРАВЛЕНО В ПАТЧЕ | 16 одновременных запросов, overflow close; запрещено стирать файл/симлинк вместо сокета; Go race тест |
| 12 | Исчерпывающий анализ потокобезопасности session/replay/queues на ARM/многопоточности | НЕ ДОКАЗАНО | Unit/race есть, нет независимой проверки всех переходов/счётчиков и многочасовой adversarial load |
| 13 | Массовые QUIC/STUN/WebRTC/DNS/IPv6 инварианты при UDP не реализованном | НЕ ДОКАЗАНО | Нужны TUN/physical capture и отсутствие обходного физического сокета |
| 14 | Секреты в crash report, logcat, metrics, операционных снимках | ЧАСТИЧНО | Есть sanitizer и редактирование ID, но нужен аудит всех путей и анализ логов/дампов/backup при экспорте |

## P1 — доступ, provisioning и масштабирование

| № | Требование | Статус | Основание/критерий закрытия |
|---|---|---|---|
| 15 | Одноразовые enrollment tokens с сроком действия и защитой от повторного использования | НЕТ | Сейчас импортируется переносимый `mbox://` с постоянным PSK; нет выделенного протокола одноразовой выдачи |
| 16 | Подписанный provisioning и отдельно установленный/pinned trust anchor | НЕТ | QR-профиль и fingerprint confirmation не гарантируют источник; `docs/SECURITY.md` |
| 17 | Client-generated asymmetric identity / привязка учётных данных к устройству | НЕТ | Есть `device_id`, но это только случайный идентификатор, не private key |
| 18 | Безопасная ротация/замена/восстановление профиля, ключей и PSK при потере телефона | ЧАСТИЧНО | Keystore, atomically committed profiles и серверный revoke есть; нет полного end-to-end enrollment/rotation workflow |
| 19 | Отключение legacy human-readable wire ID и миграция пользователей | ЧАСТИЧНО | Opaque auth handles используются, но `legacy_id` ещё может добавляться в lookup из credential file |
| 20 | O(1) инкрементальный revoke/update при 100k+ пользователей | НЕТ | `clonePoliciesLocked()` в `server/credential_store.go` копирует map O(N) при reload одного аккаунта |
| 21 | Массовая загрузка/ротация/отзыв 10/50/100/1000 пользователей | НЕ ПРОВЕДЕНО | Нужны тесты пиковых RSS/паузы/revocation latency/FD; имеющиеся unit проверки — не масштабное испытание |
| 22 | Горячая смена доверия REG.RU ↔ origin без несогласованных секретов и downtime | ЧАСТИЧНО | HMAC origin с nonce есть; полностью документированная двусторонняя смена ключей и rollover не подтверждены |
| 23 | Системный FastCGI relay: slowloris, body/header/timeout, fault injection 502/504 и нагрузка | ЧАСТИЧНО | Есть длины/сроки, нет полноценной испытательной кампании на actual shared host |
| 24 | HA: два и более origin, session affinity, draining, failover без потери очередей | НЕТ/ЧАСТИЧНО | Graceful shutdown/drain есть для одной ноды; cross-origin sticky state/external cache не реализованы |
| 25 | Разные frontend endpoints, безопасное переключение при недоступности домена | ЧАСТИЧНО | Конфиг допускает изменение URL, но автоматический multi-endpoint failover и полевая квалификация не закрыты |

## P1/P2 — маскировка, совместимость, эксплуатация и выпуск

| № | Требование | Статус | Основание/критерий закрытия |
|---|---|---|---|
| 26 | Packet-level fingerprint Chrome vs Cronet vs Go при одной сети | НЕ ПРОВЕДЕНО | Cronet подтверждён, но нет PCAP/JA4/h2/settings/size/timing сравнений |
| 27 | Реалистичная динамическая browser persona и HTTP-расписание | ЧАСТИЧНО | Cronet, padding/jitter есть; нет доказанной статистической близости к заданным клиентам |
| 28 | Проверка cover/decoy/FastCGI metadata и стабильных уникальных признаков | ЧАСТИЧНО | Есть сайт-обёртка и path shaping; нет blind fingerprint/lab/longitudinal анализа |
| 29 | Полевые испытания whitelist сетей MTS/T2/MegaFon | НЕ ПРОВЕДЕНО для текущей сборки | `docs/OPERATOR_TEST.md`, провайдеры/регион/APN/время нужно фиксировать |
| 30 | Сравнение `.fun`/`.ru` на одинаковой shared-host инфраструктуре | НЕ ПРОВЕДЕНО | Нужна контролируемая матрица; обещать эффект от TLD нельзя |
| 31 | 24 часа непрерывной работы с метриками/сверкой пакетов | НЕ ПРОВЕДЕНО | Нужен отчёт RSS/FD/streams/queues/replay/latency/error-rate |
| 32 | 7 суток непрерывной работы и деградационные сценарии | НЕ ПРОВЕДЕНО | Нужен soak с real mobile transitions и failover |
| 33 | 10/50/100 конкурентных клиентов на 1 vCPU / 1 GiB | НЕ ПРОВЕДЕНО | Реальная нагрузка до заявленного профиля памяти, устойчивость синхронизации и очередей |
| 34 | Длительный coverage-guided fuzzing всех parsers, Noise, relay/FCGI | НЕ ПРОВЕДЕНО | В CI fuzz **smoke** 10 с/таргет; crash/repro corpus и coverage report отсутствуют |
| 35 | Успешный реальный CI run в целевом GitHub, не только локальные команды | НЕ ДОКАЗАНО | Workflow есть, нужна ссылка на run и APK artifact gate |
| 36 | Release signer pinning, reproducible APK, native HEV provenance | ЧАСТИЧНО | AAR hash и debug APK есть, `check-production-apk.sh` проверяет debug signer, но нет полного pinned release signer/зависимостей |
| 37 | SBOM, dependency verification и проверяемый provenance/signatures для релиза | НЕТ/ЧАСТИЧНО | Не сформирован завершённый release supply-chain pipeline |
| 38 | Verified App Links на настоящем домене/хостинге | НЕ ДОКАЗАНО | Код intent filter есть; assetlinks.json и Android domain verification в actual deployment не подтверждены |
| 39 | Восстановление backup и rollback на копии production без простоя | ЧАСТИЧНО | Код backup test-restore есть, end-to-end disaster recovery exercise требуется |
| 40 | Независимый криптографический/protocol/Android аудит | НЕ ПРОВЕДЕНО | Не заменяется локальными тестами и подбором библиотеки Noise |

## Что считать уже частично выполненным

Есть реализация Noise NKpsk0, шифрованных UP/DOWN записей, server request sequence/ACK/replay,
строгой сериализации DOWN commit, bounded UP/stream queues, WINDOW flow control,
базовых quotas и revocation, отказа от SOCKS5 UDP ASSOCIATE/BIND, Crash-safe
Android persistent `desired_running`, Cronet over bound underlying Network,
Keystore-encrypted PSK, HMAC(REG.RU→origin), debug-JDK/Go/Python tests,
CI YAML, network handover и запуск Always-on после перезагрузки на одном Xiaomi.

Это не делает пункты 1–40 завершёнными. Особенно нельзя обещать, что
`android:exported=true`, `START_STICKY` или работающий Always-on после reboot
прошли физическую проверку отсутствия утечек.

## Дельта патча

- Android Cronet: аутентификация до заголовков/путей/тела, real-Java regression.
- Android: bounded импорт недоверенного профиля; DNS-диагностика вынесена,
  исправлена старое несоответствие CI size guard (663 > 650 строк).
- Сервер: credential JSON decoder отказывает на любых вторых JSON values/trash.
- Сервер: admin socket max 16 горутин/соединений, overflow close.
- Сервер: startup запрещает удаление regular file/symlink по admin socket path.

**Не сделано в данном патче:** UDP, signed provisioning, auth DOWN ACK, 24h/7d
soak, physical captures, TLS/browser fingerprint lab, native APK compile,
deployment и независимый аудит. Публичный релиз блокирован.
