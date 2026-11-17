# Android-клиент MBox 1.0.0-beta.4.3

[English](README.md) · [Инструкция пользователя](../docs/ru/GETTING_STARTED.md) · [Сборка и подпись](../docs/ru/RELEASE_ENGINEERING_BETA43.md)

Для пользователя: **Android 10+ (API 29), ARM64**. Получите подписанный APK из [GitHub Releases](https://github.com/RegSirius06/mbox/releases) **после публикации**, импортируйте профиль `mbox://` или HTTPS-подписку и нажмите Connect.

Приложение поддерживает **AUTO (QUIC → HTTPS)**, русскую/английскую локализацию, системные светлую/тёмную темы, управление подписками, действие Disconnect в уведомлении и скрытую диагностику по семи нажатиям на MBox. QUIC использует streams, **не** DATAGRAM; UDP приложений в HTTPS-транспорте передаётся внутри шифрованных MBox-записей поверх TCP. Переключение может оборвать действующие сеансы.

**Разработчику на Arch:**

```bash
cd ~/src/mbox-main
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
python3 -m pytest -q tests
GO111MODULE=off go test ./client ./server ./relay
cd android
./gradlew :app:assembleRelease
```

Gradle `assembleRelease` сам по себе **не подписывает** итоговый публичный APK. Для подписи из корня проекта используется `./tools/build-release-apk.sh` с переменными `MBOX_RELEASE_KEYSTORE`, `MBOX_RELEASE_KEY_ALIAS`, `MBOX_RELEASE_SIGNER_SHA256` и прежним приватным ключом. Полная команда, проверка `apksigner`, SHA-256 и `adb install -r` — в [релизном руководстве](../docs/ru/RELEASE_ENGINEERING_BETA43.md).

**Не удаляйте старую beta.4.2** перед обновлением: приложение и credential storage могут потерять настройки. Не публикуйте debug APK, тестовые QUIC endpoints или профили с секретами.
