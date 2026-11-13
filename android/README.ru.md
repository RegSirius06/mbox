# Android-клиент MBox 1.0.0-beta.4.1

> **beta.4.1:** проверяйте финальный APK с production-подписью из точного релизного коммита. Не копируйте SHA-256 файла beta.4. Тесты исходников и телефона — не физический аудит утечек.

[English](README.md) · [Руководство](../docs/ru/ANDROID_BETA4_1.md) · [Подпись и сборка](../docs/ru/ANDROID_RELEASE_ENGINEERING.md)

**Android 10+ (API 29), ARM64.** Один обычный интерфейс у debug и release; в debug дополнительные лабораторные функции находятся в настройках. Статус показывает подключение и текущий транспорт.

- **Auto:** предпочтительно QUIC, при ошибке fallback на HTTPS, при смене Wi-Fi/мобильной сети попытки переоцениваются.
- **QUIC:** прямой внешний UDP/HTTP3 к endpoint из импортированного профиля. Внутри QUIC применяются streams, не DATAGRAM.
- **HTTPS:** Cronet HTTP/2/TCP через frontend/FastCGI, UDP приложений внутри Noise/MBox поверх TCP.

## Установка

Скачайте подписанный APK из GitHub Releases. Если раньше стояла debug-сборка с другим сертификатом, Android выдаст `INSTALL_FAILED_UPDATE_INCOMPATIBLE`. **Сначала** сохраните приватный профиль, после чего можно удалить старую debug-сборку и установить release. Обычные обновления со стабильным сертификатом выполняются без удаления данных.

Для работы QUIC необходима пара полей профиля `transport.quic_url`, `transport.quic_ca_pem`. Старые HTTPS-only профили сохраняют возможность подключения. Настройки сервера **не зашиваются** в release APK.

## Сборка на Arch

```bash
cd ~/src/mbox-main
./android/tools/build-debug.sh
# Для подписанного выпуска (keystore и пароль никогда не коммитятся):
export MBOX_RELEASE_KEYSTORE="$HOME/.local/share/mbox-signing/mbox-release.p12"
export MBOX_RELEASE_KEY_ALIAS=mbox-release
export MBOX_RELEASE_SIGNER_SHA256=17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586
./tools/build-release-apk.sh
```

VPN работает через `VpnService`/TUN/HEV и локальный Go core. Для защиты при обрыве включите системные Always-on и Lockdown, затем проверьте их. Полный аудит DNS/IPv4/IPv6 утечек не завершён.
