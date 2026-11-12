# Сборка Android release — MBox 1.0.0-beta.4

[English](../ANDROID_RELEASE_ENGINEERING.md) · [Чек-лист](../../RELEASE_CHECKLIST.ru.md)

Собирать и подписывать APK нужно на доверенной машине разработчика (Arch). Постоянный keystore хранится **вне** Git-репозитория. Не публикуйте пароли и приватные ключи.

- `VERSION=1.0.0-beta.4`, `versionName=1.0.0-beta.4`, `versionCode=7`.
- В **release** ARM64 APK должны входить `libmbox.so` и `libmboxquic.so`, а не только в debug.
- Файлы `src/debug/assets/mbox-quic-remote.txt` и `mbox-quic-ca.pem` **не должны** попадать в release. QUIC endpoint и доверенный X.509 PEM берутся из импортированного `mbox://` профиля.
- `enable_udp` — штатный флаг beta 4; опасные debug/network-debug/insecure опции в release недоступны.
- `applicationId=io.mbox.android`, сертификат подписи APK SHA-256: `17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586` (издатель APK, **не** QUIC-сервер).

```bash
cd ~/src/mbox-main
export MBOX_RELEASE_KEYSTORE="$HOME/.local/share/mbox-signing/mbox-release.p12"
export MBOX_RELEASE_KEY_ALIAS=mbox-release
export MBOX_RELEASE_SIGNER_SHA256=17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586
./tools/build-release-apk.sh
./tools/check-production-apk.sh dist/release/MBox-1.0.0-beta.4-android-arm64.apk
```

Скрипт не перезаписывает существующий APK: перед новой сборкой старую версию нужно **осознанно** отложить. У новой сборки будет другой SHA-256 файла — обновите его в release notes и GitHub. Перед публикацией проверяйте на телефоне **подписанный APK**, а не только debug. Если Android отказывает с `INSTALL_FAILED_UPDATE_INCOMPATIBLE`, причина в разных подписях; до удаления debug обязательно сохраните приватный профиль.

Физические проверки DNS/UDP/IPv4/IPv6-утечек и длительный soak открыты; срок тестового QUIC-сертификата — **07.12.2026**.
