# Чек-лист выпуска MBox v1.0.0-beta.4.3

[English](RELEASE_CHECKLIST.md) · [Подробные команды](docs/ru/RELEASE_ENGINEERING_BETA43.md)

Это **prerelease**, не RC1 и не независимый аудит VPN. Отмечайте пункт только по наблюдаемому результату на **финальном коммите**.

## Исходники и версии

- [ ] `VERSION=1.0.0-beta.4.3`, `versionName=1.0.0-beta.4.3`, `versionCode=10`, `io.mbox.android`; `mboxctl` и APK gate совпадают.
- [ ] Нет незапрошенного изменения Noise/wire, API origin и форматов профилей/подписок; старые beta.4.2 профили проверены.
- [ ] `git status`, `git diff --check`, ручная проверка diff/секретов; не коммитятся APK, профили, ключи и runtime DB.
- [ ] Полные Python-тесты, Go/race client/server/relay, native-QUIC vet/race и Android Release успешно выполнены на финальном коммите.
- [ ] `python3 tools/check-release-docs.py` прошёл; актуальные EN/RU документы согласованы, исторические сохранены.

## Подпись и Android

- [ ] Прежний PKCS12 keystore (вне Git) найден; сертификат совпадает с доверенным SHA-256 пином.
- [ ] Существующий APK beta.4.3 предварительно сохранён; новый собран **и реально подписан**, не только `BUILD SUCCESSFUL` Gradle.
- [ ] `tools/check-production-apk.sh` проверил новый APK, manifest, отсутствие debug assets, подписанта и native core/QUIC; `.sha256` совпал.
- [ ] `adb install -r` поверх предыдущей подписанной beta сохраняет профили и подписки, не требует переустановки.
- [ ] На реальном телефоне проверены светлая/тёмная тема (включая Connect/Disconnect), RU/EN, фирменная иконка, скрытая диагностика, уведомления и их кнопка Disconnect.
- [ ] Проверены QUIC, HTTPS, AUTO/fallback, смена и потеря сети, экран выключен, длительный простой, восстановление после сбоя.

## Сервер и эксплуатация

- [ ] Различие между версией Git/Android и **установленным VPS** честно отражено в release notes.
- [ ] При необходимости origin обновлён отдельно, с backup, планом отката и согласованным перерывом; `mboxctl version` на VPS подтверждён.
- [ ] Админка проверена через SSH; публичный домен **не** включён автоматически; REG.RU shared hosting не затронут без отдельного согласования.
- [ ] Незавершённые проверки: физический capture на IPv4/IPv6/DNS/UDP, Android Lockdown, TLS-ротация и soak — явно перечислены в релизе.

## Публикация

- [ ] RU и EN README, Release Notes, changelog, release body и каталог документов актуальны.
- [ ] GitHub CI зелёный на итоговом SHA; создан тег `v1.0.0-beta.4.3` именно этого коммита.
- [ ] GitHub Release помечен **Pre-release**; приложены **только** подписанный ARM64 APK и `.sha256`.
- [ ] В release body подставлены **реальные** APK SHA-256 и Git SHA; нет `PENDING`, секретных ссылок или приватных логов.
