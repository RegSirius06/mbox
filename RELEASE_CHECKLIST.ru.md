# Выпуск MBox v1.0.0-beta.4

[English](RELEASE_CHECKLIST.md) · [Release notes](RELEASE_NOTES.ru.md)

**Здесь не выполняется автоматический деплой на RU VPS, REG.RU или в GitHub.** Команды выполняются на Arch после проверки актуального локального дерева.

## 1. Согласованность дерева

```bash
cd ~/src/mbox-main
cat VERSION
# Ожидается 1.0.0-beta.4
git diff --check
python3 -m pytest -q tests
(cd client && GO111MODULE=off go test -race ./... && GO111MODULE=off go vet ./...)
(cd server && GO111MODULE=off go test -race ./... && GO111MODULE=off go vet ./...)
(cd native-quic && go test -race ./... && go vet ./...)
./tools/secret-scan.sh
```

Если `secret-scan` находит **литералы** `mbox://` в README/тестах, проверить находки отдельно; не объявлять это автоматически утечкой, но и не пропускать реальные ссылки с ключами.

## 2. Проверить собранный APK

```bash
APK=dist/release/MBox-1.0.0-beta.4-android-arm64.apk
sha256sum "$APK"
export MBOX_RELEASE_SIGNER_SHA256=17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586
./tools/check-production-apk.sh "$APK"
```

SHA-256 файла должен быть `c3b1cbcb1b4b472aba35237657c2834a2aa8f032d861cbf817f5a5b9f18b43e1` **только если APK не пересобирался**. В случае другого хеша заново проверить APK и заменить хеш во всех release notes, GitHub Release и `.sha256`.

## 3. Смоук-тест release на физическом Android

Импорт профиля и новый UI; ручной QUIC на Wi-Fi, ручной HTTPS, Auto Wi-Fi↔мобильная сеть; DNS, HTTPS и Telegram; при возможности UDP/STUN и второй оператор Tele2; Always-on/Lockdown. Записывать **реально сделанные** проверки, не подменять debug результатами. Отсутствие полного физического leak-теста указать как ограничение.

## 4. Коммит, тег, GitHub

Убедись, что итоговый код и документы уже находятся в `main` (при необходимости сперва слей рабочую ветку через PR с зелёными CI). Сначала проверь `git status --short` и внимательно посмотри `git diff --cached --stat`; нельзя коммитить `.p12`, `mbox://`, production-конфигурации. Не делай `git add -A` вслепую. После коммита и чистого дерева:

```bash
test "$(git branch --show-current)" = main || { echo "Сначала слей изменения в main"; exit 1; }
git tag -a v1.0.0-beta.4 -m 'MBox 1.0.0-beta.4 Android prerelease'
git push origin main
git push origin v1.0.0-beta.4
sha256sum "$APK" > "$APK.sha256"
gh release create v1.0.0-beta.4 "$APK" "$APK.sha256" --verify-tag --prerelease --latest=false --draft --title 'MBox 1.0.0-beta.4 — Android beta' --notes-file docs/GITHUB_RELEASE_BODY.md
# После проверки черновика:
gh release edit v1.0.0-beta.4 --draft=false
```

Внимание: `gh` требует авторизацию и права записи в репозиторий. Не создавать тег до завершения тестов; опубликованные теги и бинарники не заменять молча.
