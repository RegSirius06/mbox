# Выпуск MBox 1.0.0-beta.4.3: сборка, подпись, публикация

[English](../RELEASE_ENGINEERING_BETA43.md) · [Чек-лист](../../RELEASE_CHECKLIST.ru.md)

**Все операции сборки — на Arch разработчика.** Секретный keystore не размещается в Git. Этот документ не запускает серверное обновление, создание тега или GitHub Release.

## Версии и соответствие компонентов

В финальном коммите проверяем:

- `VERSION`: `1.0.0-beta.4.3`;
- `android/app/build.gradle`: `applicationId 'io.mbox.android'`, `versionCode 10`, `versionName '1.0.0-beta.4.3'`;
- `tools/mboxctl/mboxctl.py`: `mboxctl version` сообщает beta.4.3;
- `tools/build-release-apk.sh` и `tools/check-production-apk.sh`: проверяют beta.4.3/10;
- актуальные RU/EN README, Release Notes, Checklists, статус и документация описывают beta.4.3;
- сервер origin на VPS показывает beta.4.3 **только после явного обновления**. Пока там beta.4.2, не пишем «весь production обновлён».

Исторические примечания beta.4.1/4.2 остаются историческими; их номера менять не надо.

## Проверки исходников

```bash
cd ~/src/mbox-main
set +e  # В интерактивной оболочке не оставлять set -e включённым

git status --short
git diff --check
python3 -m pytest -q tests
GO111MODULE=off go test -race ./client ./server ./relay
(
  cd native-quic
  go vet ./...
  go test -race -count=1 ./...
)
bash -n install.sh tools/relay-setup.sh tools/build-release-apk.sh
python3 tools/check-release-docs.py
```

Проверить изменения и отсутствие приватных данных. `tools/secret-scan.sh` может выводить **предупреждения** по документированным примерам `mbox://` и не заменяет ручной аудит, но его тоже стоит запустить. Не выполнять `git add -A` без просмотра статуса и исключений для приватных файлов.

## Release APK с прежним сертификатом

**Ранее проверенный SHA-256 сертификата:**
`17FD9F930B8356BECA8251DD215220771A3D020E56FB6A703B526C35CCCD7586`

Это пин сертификата, **не SHA-256 APK**. Он должен совпасть с доверенным ранее записанным отпечатком. Команда `keytool` запросит пароль **локально**:

```bash
cd ~/src/mbox-main
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
export MBOX_RELEASE_KEYSTORE="$HOME/.local/share/mbox-signing/mbox-release.p12"
export MBOX_RELEASE_KEY_ALIAS='mbox-release'
export MBOX_RELEASE_SIGNER_SHA256='17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586'

test -f "$MBOX_RELEASE_KEYSTORE" && echo 'KEY FOUND' || echo 'KEY MISSING: stop before signing'
keytool -list -v -storetype PKCS12 \
  -keystore "$MBOX_RELEASE_KEYSTORE" \
  -alias "$MBOX_RELEASE_KEY_ALIAS" | grep -E 'Alias name:|SHA256:'
```

Не передавайте пароль как аргумент CLI, через чат или в переменной окружения. `tools/build-release-apk.sh` запросит его через штатный `apksigner`.

**ВАЖНО:** если APK beta.4.3 уже существует, скрипт намеренно **не перезапишет его**. Перед повторной сборкой переместите предыдущие APK и `.sha256` в резервную копию:

```bash
cd ~/src/mbox-main
APK='dist/release/MBox-1.0.0-beta.4.3-android-arm64.apk'
BACKUP="$HOME/mbox-apk-backups/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p -- "$BACKUP"
for f in "$APK" "$APK.sha256"; do
  if [[ -f "$f" ]]; then mv -- "$f" "$BACKUP/"; fi
done
./tools/build-release-apk.sh
```

Скрипт строит core + native QUIC, release Android, выполняет `zipalign`, подписывает **старым** сертификатом и проверяет release gate. Сообщение Gradle `BUILD SUCCESSFUL` **не означает**, что новая APK подписана: проверяйте итог `Release APK ready`.

## Проверка результата и обновление устройства

```bash
cd ~/src/mbox-main
APK='dist/release/MBox-1.0.0-beta.4.3-android-arm64.apk'
test -s "$APK" && echo "SIGNED APK FOUND" || echo "SIGNED APK MISSING: stop here"
./tools/check-production-apk.sh "$APK"
( cd dist/release && sha256sum -c MBox-1.0.0-beta.4.3-android-arm64.apk.sha256 )
sha256sum "$APK"
adb install -r "$APK"
```

Проверить, что на устройстве сохранились профили/подписки и что переключение светлая ↔ тёмная при работающем VPN не прерывает соединение. Пройти QUIC, HTTPS, AUTO/fallback, потерю/смену сети, длительный простой, кнопку Disconnect в уведомлении и восстановление. Сборка без ошибки и 273 Python-теста **не заменяют испытания конкретного APK**. Отдельно оставьте открытыми проверки DNS/IPv4/IPv6/UDP утечек на физическом интерфейсе, если они не выполнены.

## Что делать с VPS и REG.RU

Сейчас обновлённая админка работает на VPS, но `mboxctl version` ранее сообщал beta.4.2. Это **реальное различие развернутых компонентов**. Серверную часть обновляем **отдельным подтверждённым действием** после backup и проверки `./install.sh --check` на VPS. Режим `--upgrade-origin` **перезапускает origin** и может оборвать пользователей. Не объединять его автоматически с публикацией APK. REG.RU shared hosting — отдельная площадка; мастер интерактивного fresh-install не должен автоматически трогать ранее настроенный сайт.

## Финальная публикация

Только после приёмки: коммит финальных источников/доков, push, зелёный CI, тег `v1.0.0-beta.4.3`, GitHub Release с пометкой **Pre-release**. Прикладываем **только подписанный** ARM64 APK и `.sha256`. Внесите фактические SHA-256 APK и Git SHA в черновики `docs/ru/GITHUB_RELEASE_BODY.md` и `docs/GITHUB_RELEASE_BODY.md`; не публикуйте `PENDING`. Никаких приватных ключей, `mbox://` профилей или журналов с секретами.
