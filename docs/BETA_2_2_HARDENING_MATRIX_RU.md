# MBox 1.0.0-beta.2.2 — матрица hardening (кандидат, 5 ноября 2026)

**База:** коммит `68f624c` + серия совместимых изменений beta.2.2.
**Реальный production:** этот документ и патч **ничего не развёртывают** на RU VPS, REG.RU или Android.
**Важно:** наличие кода и локальный Go/Python green не подтверждают отсутствие физических сетевых утечек или устойчивость 24/7.

Статусы: **К** — реализация в исходниках есть и существуют автоматические проверки; **Ч** — часть контракта/тестов есть; **Н** — обязательная часть не реализована; **В** — внешняя/длительная приёмка не проведена. К не означает эксплуатационную сертификацию.

| № ТЗ | Тема | Статус | Что нужно для полного закрытия |
|---:|---|:---:|---|
| 1 | Общая цель 24/7, multi-user, отсутствие утечек | Ч | Полная приёмка ниже |
| 2 | Полный аудит и правила разработки | Ч | Независимый проход всех подсистем и обязательных gates |
| 3 | DOWN budget `globalQueuedBytes` | К | Длительный soak и полная инвариантная проверка освобождения |
| 4 | Android UDP | Н | Сейчас UDP ASSOCIATE/BIND отклоняются; нужен шифрованный datagram protocol + capture |
| 5 | Android VPN fail-closed | Ч/В | Wi-Fi/LTE/IPv6/TUN/physical capture, Lockdown, crash/force-stop |
| 6 | Opaque wire identity | Ч | Отключить legacy_id после миграции, проверить идентификацию вне Noise |
| 7 | Одноразовый подписанный provisioning | Н | Enrollment, client-generated key pair, подпись profile, short-lived token |
| 8 | Origin trust boundary | Ч | HMAC от relay есть; отдельный per-source limiter/firewall и rollover тест |
| 9 | `sessionOpenRates` bounded TTL | К | Подтвердить под долговременной нагрузкой |
| 10 | FastCGI path/lock hardening | К | Fuzz/нагрузка на настоящем REG.RU |
| 11 | HTTP в production | К | Android release APK acceptance отдельно |
| 12 | Incremental multi-user credentials | Ч | O(1) map update есть, revoke проверяет сессии; 100k/rotation soak нет |
| 13 | Sticky routing / HA / drain | Н | Single-node drain есть, multi-origin routing/failover нет |
| 14 | FastCGI slots scaling | В | Проверки 1/10/50/100/500/1000 клиентов |
| 15 | Модель TLS/H2/HTTP/traffic fingerprint | Ч | Документирована, но статистические базовые измерения не закончены |
| 16 | Fingerprint regression laboratory | Н | Chrome vs Cronet vs Go PCAP/JA3/JA4/H2/timing suite |
| 17 | Adaptive traffic shaping | Ч | Jitter, batching есть; stateful режимы и бюджеты не реализованы полностью |
| 18 | Cover threat model и budget | Ч | Серверный cover есть, нет полного клиентского adaptive/low-battery/metered бюджета |
| 19 | Noise padding/statistical audit | Ч | Padding есть, нет требуемых распределений/корреляционных измерений |
| 20 | Deterministic transport production guard | К | Есть guard developer mode; проверить подписанный APK |
| 21 | Network debug production guard | Ч | Есть guard, redaction; аудит logcat/native/third-party остаётся |
| 22 | Secrets at rest | Ч | Android PSK в Keystore; полная модель token/private-key/backup не закрыта |
| 23 | Centralized logging/redaction | Ч | Тесты redaction есть, нет доказанной полной coverage логов |
| 24 | Session ID entropy/fixation | Ч | Случайные SID и Noise OPEN есть; независимый cryptographic review нужен |
| 25 | Noise protocol / negative tests | Ч | Noise NKpsk0, replay/counter tests; внешний аудит и длинный fuzz нужны |
| 26 | Fuzz всех parser surfaces | Ч | Есть fuzz targets и новые тесты query; полный список ещё не покрыт |
| 27 | Resource ceilings | Ч | Базовые queues/streams/session caps; новый HTTP inflight cap, но OS/relay pressure не закрыт |
| 28 | Goroutine leaks | В | Нет 100k connect/disconnect и полного baseline-подтверждения |
| 29 | FD/socket leaks | В | Нет многоциклового Linux integration/Android handover отчёта |
| 30 | 24h/72h/7d heap soak | В | Нет длительных результатов с ресурсной телеметрией |
| 31 | Reconnect storm | В | Нет 100/500/1000/5000 клиентов одновременно |
| 32 | Credential rotation under load | Ч | Отзыв есть, sustained load с независимыми пользователями не проверен |
| 33 | Supply chain HEV/Gradle/Actions/SBOM | Ч | Actions закреплены SHA, HEV hash есть; исходная сборка, verification и SBOM не готовы |
| 34 | Secure release APK/AAB | Ч | Gate требует pinned signer SHA-256; reproducible build/provenance нет |
| 35 | Observability | Ч | Aggregate status/metrics есть; распределения latency/saturation и full dashboard нет |
| 36 | Readiness/liveness | Ч | Endpoint существует; полный credential-store/critical dependencies contract не закрыт |
| 37 | Graceful shutdown/drain | Ч | Есть drain и lifecycle tests; FD/goroutine/replay подтверждение на реальной нагрузке отсутствует |
| 38 | Config fail-fast validation | Ч | Есть проверки, но аудит всех нулевых/вредных параметров ещё нужен |
| 39 | File permissions/no symlinks | Ч | Отдельные credential/relay guards есть; системная проверка всех путей отсутствует |
| 40 | Документирование trust boundaries | К | Документы SECURITY/STEALTH и transport model есть; подтвердить внешним аудитом |
| 41 | Opaque frontend HTTP envelope | Н | HTTP `seq/ack/cmac` видим REG.RU; нужен согласованный wire v2 |
| 42 | HTTP semantics/browser mismatch | Ч | Android Cronet, но нет fingerprint lab и полной проверки заголовков |
| 43 | Cronet regression | Ч | Есть Java/unit/network tests, нужны реальные 502/504/handover/cert failures |
| 44 | Camouflage не должен обходить security | Ч | Операции отделены; требуется отдельный fail-open review |
| 45 | Documentation coverage | Ч | Много docs уже есть, не все обязательные разделы закрыты |
| 46 | Security test matrix | Ч | Эта матрица gap, не является threat-by-threat acceptance record |
| 47 | CI/nightly gates | Ч | Pinned Actions, Go/Python/Android jobs есть; SBOM, long fuzz, real device gates не полные |
| 48 | DoD каждого бага | Ч | Новые баги сопровождаются тестами; независимый соседний аудит не завершён |
| 49 | Общий Definition of Done | Н | Нельзя объявить: UDP, enrollment, capture, HA, supply chain, soak не закрыты |
| 50 | Порядок phases | Ч | Продолжать P0/P1 → operations → privacy → supply-chain |
| 51 | Малые commits | К | Серия поставляется отдельными коммитами + `git am` патчем |
| 52 | Формат отчёта агента | Ч | Изменения и риски ниже; все 55 закрытыми не объявляются |
| 53 | v1/v2 migration compatibility | Ч | Текущая серия совместима с HTTP Noise beta.2; v2 UDP/enrollment требует плана |
| 54 | Финальный независимый security review | В | Требуется после полного implementation |
| 55 | Приоритет безопасность → metadata → camouflage | Ч | Подтверждается намерение, но нет доказательства всех инвариантов |

## Дельта данной серии относительно `68f624c`

| Severity | Root cause | Files | Fix | Regression test | Status |
|---|---|---|---|---|---|
| P1 DoS | Origin принимал/буферизовал тела неаутентифицированных запросов до дешёвой проверки заголовков, без global in-flight budget | `server/origin_auth.go`, `server/config_runtime.go`, `tools/mboxctl/mboxctl.py` | Header preflight + `max_concurrent_http_requests` + aggregate rejection metric | `server/origin_admission_test.go` | Автоматически проверено, нужен live load |
| P1 CPU | Replay nonce map сканировалась при каждом запросе, O(N) | `server/origin_auth.go`, `server/maintenance.go` | Amortized TTL sweep + O(1) saturation deny + idle cleanup | `TestOriginNonceCacheIsBounded` (дополнить load) | Автоматически проверено, нужен live load |
| P1 parser | Duplicate/encoded/unknown query args могли иметь неоднозначную семантику | `server/control_query.go`, `server/http_handlers.go` | Reject duplicate, malformed, unexpected or noncanonical query values | `server/control_query_test.go` fuzz seeds | Автоматически проверено |
| P1 supply chain | Non-debug APK signature DN не фиксирует доверенную release identity | `tools/check-production-apk.sh` | Mandatory pinned signer SHA256, exactly one signer | `tests/test_apk_release_gate.py` | Автоматически проверено на mock SDK; реальный release signer пока не проверен |
| P2 operations | Ручные копирования могли оставить новый бинарник на диске при старом запущенном процессе или без отката | `tools/deploy-origin-root.sh` | Dry-run, existing-root check, backup/rollback, SHA /proc/PID/exe | `bash -n`, требуется live dry-run | Подготовлено, не развёрнуто |
| P2 version | Android versionName/code отставали от server/tooling | `VERSION`, `android/app/build.gradle` | `beta.2.2` / versionCode 5 | APK сборка на Arch требуется | Код исправлен |

## Ограничения и новые параметры

- `[limits] max_concurrent_http_requests = 64` — лимит одновременно обрабатываемых запросов. При достижении лимита origin возвращает `503 Retry-After: 1`, возможны дополнительные ретраи при пиках. Измерить перед повышением.
- Replay-cache nonce пока ограничен 65 536 записями на двухминутную TTL. **Это может стать bottleneck для сотен/тысяч активно опрашивающих клиентов.** При заполнении сервер безопасно отвергает новые запросы до sweep; оптимизация CPU не превращает этот лимит в неограниченную масштабируемость.
- `cmac` на уровне HTTP **не зашифровывает** `seq/ack` для REG.RU. Строгий режим уже был включён оператором до этой серии.
- Android target API 36, Android SDK, физические packet captures, REG.RU, production RU VPS и многочасовые тесты **здесь недоступны**.
- Android подписывается прежним ключом только при сборке в прежнем окружении. Новый `versionCode=5` требует подписи тем же ключом для `adb install -r`.
- Совместимость с *прежним уже обновлённым MBox Android*, который отправляет `cmac`, предусмотрена. APK, который реально не отправляет `cmac`, при включённом strict режиме работать не сможет, независимо от `versionName`.

## Решение о выпуске

Серия может быть предложена как **исходный код beta.2.2 candidate для тестирования**, но **не как security-qualified pre-release для широкого распространения**. Не выпускать подписанный публичный APK и не объявлять ТЗ закрытым до P0 Android capture, протоколирования/политики UDP, provisioning, нагрузки и supply-chain gates. Бета-номер сам по себе не даёт гарантии совместимости и отсутствия утечек.
