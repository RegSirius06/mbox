# Сборка и подпись Android — MBox 1.0.0-beta.4.3

[English](../ANDROID_RELEASE_ENGINEERING.md) · [Полное релизное руководство](RELEASE_ENGINEERING_BETA43.md)

Чтобы не было двух противоречивых инструкций, **актуальные команды** находятся в [руководстве по сборке beta.4.3](RELEASE_ENGINEERING_BETA43.md).

**Параметры:** `io.mbox.android`, `versionName=1.0.0-beta.4.3`, `versionCode=10`, Android 10+ ARM64. В релизном APK должны находиться `lib/arm64-v8a/libmbox.so` и `libmboxquic.so`, но не должны присутствовать debug-профили/сертификаты. Нельзя публиковать debug или unsigned APK.

Для косметической beta.4.3 используем **прежний production PKCS12-сертификат**, находящийся вне Git. Пароль вводится локально. Если APK уже существует, скрипт намеренно не перезаписывает его — сначала сохраните предыдущий файл. `BUILD SUCCESSFUL` в Gradle **не равен** «APK подписан». Проверяем `tools/check-production-apk.sh`, `sha256sum -c` и обновление поверх старой версии на Android. Независимый аудит утечек и гарантии обхода DPI не заявляются.
