# MBox v1.0.0-beta.4.4 — финальная проверка

[English](RELEASE_CHECKLIST.md) · [Примечания](RELEASE_NOTES.ru.md) · [Описание релиза](docs/ru/GITHUB_RELEASE_BODY.md)

**Prerelease, не RC1.** Все пункты проверяются на **последнем коммите и подписанном APK**, не по предыдущей сборке.

## Исходники, безопасность, документация

- [ ] `VERSION=1.0.0-beta.4.4`, Android `versionName=1.0.0-beta.4.4`, `versionCode=11`, `io.mbox.android`; версия CLI и проверка подписи согласованы.
- [ ] В Git нет PSK, `mbox://`, токенов подписок, keystore, server.json, локального egress.json и БД; `git diff --check`, `tools/secret-scan.sh` успешны.
- [ ] `python3 tools/check-release-docs.py`, `python3 tools/check-source-layout.py` успешны; актуальные RU/EN документы не противоречат beta.4.4. Исторические страницы beta.4.1–4.3 не переписываются.
- [ ] `GO111MODULE=off go test -race ./common/dgram ./common/noise ./client ./server ./relay`, vet; `(cd native-quic && go vet ./... && go test -race -count=1 ./...)`; `python3 -m pytest -q tests` — все ошибки исправлены либо явно оформлены.
- [ ] `bash -n install.sh`, `bash install.sh --check` пройдены. **Настоящая fresh-install и upgrade/rollback проверяются отдельно на чистом VPS/staging**: read-only --check не подтверждает полный установщик.

## Android, транспорты, совместимость

- [ ] APK пересобран и подписан **прежним** сертификатом; `tools/check-production-apk.sh` и `.sha256` проходят проверку.
- [ ] `adb install -r` поверх production-signed beta.4.3 сохраняет профили/подписки; RU/EN, light/dark, отступы «Выбрать профиль», скрытая диагностика работают.
- [ ] QUIC/HTTPS, AUTO fallback, потеря/смена сети, сон экрана, восстановление и повторный DATAGRAM attach в той же Noise-сессии проверены.
- [ ] UDP AUTO / STREAMS only / DATAGRAM only (нет UDP fallback в обе стороны), TCP и управление проверены на новом origin.
- [ ] Подписка v1 для beta.4.3, v2 для beta.4.4, форматы `MBD1/MBD2`; одна ссылка отдаёт DK/KZ, реальные внешние страны соответствуют профилям, default жив у старого клиента.
- [ ] **После публикации** на beta.4.3 проверено уведомление об обновлении: ссылка на новый релиз, действие отмены, отсутствие навязчивых повторов и поведение без разрешений уведомлений.

## Production и нерешённые проблемы

- [ ] Origin обновлён отдельно с backup/rollback; сокет DATAGRAM/ACL, старые клиенты, данные и ключи целы; admin, подписки, REG.RU отдельны.
- [ ] `tools/build-relay.sh` собрал и проверил relay; это **не** доказательство безопасного обновления REG.RU и соблюдения CPU-квот.
- [ ] `egress.json` после редактирования из панели читается `mbox` (`root:mbox 0640`), egress allowlist и default route проверены.
- [ ] Issues: CPU/воркеры REG.RU, измерение батареи Android, физические IPv4/IPv6/DNS/UDP и Lockdown, уведомление GitHub, UX выходов.

## Публикация

- [ ] `main` GitHub содержит финальный прошедший CI исходник, тег `v1.0.0-beta.4.4` указывает на тот же SHA.
- [ ] GitHub Release помечен **Pre-release**. Только подписанный ARM64 APK и SHA256, без debug и секретов. Заполнены `PENDING_*` в текстах релиза.
- [ ] Сохранены APK SHA, тег SHA, результаты установки/проверки и ссылки на Issues; скачивание и уведомление проверены на старом клиенте.
