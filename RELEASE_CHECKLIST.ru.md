# Памятка публикации `1.0.0-beta.3`

[Полные команды и проверки на английском](RELEASE_CHECKLIST.md)

**Схема:** исходники, патчи, Go-тесты, Gradle и `gh` — на Arch; origin на RU VPS под `root`; сайт/relay на REG.RU shared hosting отдельно. Публичная beta.2.1 несовместима с сервером со строгим `cmac`.

## Перед тегом

1. Исходники beta.3: `VERSION`, `mboxctl`, Android `versionName=1.0.0-beta.3`, `versionCode=6`; чистый git; Go vet/race, Python tests, secret scan, CI.
2. Пересобрать и установить debug APK на **свой** Android, проверить reconnect, Telegram, автозапуск и работу через REG.RU. Debug APK в публичный релиз **не загружать**.
3. RU VPS: собрать сервер на Arch, выполнить `tools/deploy-origin-root.sh --host root@RU_IP` и затем `--apply`. Проверить `mboxctl doctor`, SHA-256 процесса и `auth.require_authenticated_control=true`. REG.RU не менять при этой версии.
4. Выпустить **отдельный подписанный release APK** через `tools/build-release-apk.sh`: Keystore и пароль хранить вне Git, задать `MBOX_RELEASE_SIGNER_SHA256`. Скрипт проверяет release сертификат, отсутствие debug-флага и сохраняет SHA-256 APK.
5. Коммит и безопасный push в `main` без `--force`; аннотированный тег `v1.0.0-beta.3`; `gh release create ... --prerelease --notes-file RELEASE_NOTES.ru.md` с APK и `.sha256`. Полные команды в английском чеклисте.

Если на телефоне был debug-signed APK, release-signed другой подписью не установится поверх него. До удаления приложения экспортировать/сохранить профиль и только затем удалять. Один и тот же release-key используйте для последующих обновлений.

## Нельзя заявлять как проверенное

Полное отсутствие TCP/DNS/UDP/IPv6 утечек, Always-on/Lockdown при аварийном завершении, нативный UDP, универсальный обход DPI, долгий 24/7 soak, защищённый одноразовый enrollment и закрытие всех 55 пунктов ТЗ. 05.11.2026 зафиксирован успешный полевой тест **одной** связки Android–REG.RU–RU VPS во время активных БС. Публичная beta.3 — *experimental prerelease*, не security certification.
