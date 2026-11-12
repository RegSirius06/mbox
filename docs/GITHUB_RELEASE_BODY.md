# MBox 1.0.0-beta.4 — Android prerelease

**Android 10+ / ARM64.** Signed release APK (`io.mbox.android`, `versionCode 7`). New: native QUIC/HTTP3 over UDP, Cronet HTTPS fallback, Auto transport selection, tunneled application UDP and a simplified Android UI. QUIC configuration comes from your personal profile, not from the APK.

- APK SHA-256: `c3b1cbcb1b4b472aba35237657c2834a2aa8f032d861cbf817f5a5b9f18b43e1`
- APK signing cert SHA-256: `17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586`
- Maintainer tests: 110 Python passed, signed release built/installed; debug tested Wi-Fi QUIC and restricted MTS/MegaFon HTTPS/Auto. **Full signed-release field matrix, Tele2, physical leak capture and long soak are not complete.**
- To avoid traffic exposure during outages, enable **Always-on VPN + Block connections without VPN** and test your device. Independent security audit is pending.
- The test QUIC server certificate expires on **December 7, 2026**; the operator must renew it and issue updated trusted profiles.

**Do not submit full `mbox://` provisioning links, private keys or unredacted logs.** Switching from debug to release signing may require uninstall after a secure profile backup.

[English guide](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4/docs/ANDROID_BETA4.md) · [Full release notes](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4/RELEASE_NOTES.md)

---

# MBox 1.0.0-beta.4 — Android (русский)

**Android 10+ / ARM64.** Подписанный APK. Новое: прямой QUIC/HTTP3 поверх UDP, резервный HTTPS/Cronet, Auto, передача UDP приложений внутри MBox и простой интерфейс. QUIC-адрес и сертификат берутся из личного профиля, в APK они не зашиты.

- SHA-256 APK: `c3b1cbcb1b4b472aba35237657c2834a2aa8f032d861cbf817f5a5b9f18b43e1`
- SHA-256 сертификата подписи: `17fd9f930b8356beca8251dd215220771a3d020e56fb6a703b526c35cccd7586`
- Проверено разработчиком: 110 Python-тестов, подписанный APK собран и установлен; на **debug** — QUIC по Wi-Fi и HTTPS/Auto на МТС/МегаФон с ограничениями. **Полная проверка именно release по всем сетям, Tele2, физический тест утечек и долгий soak ещё не выполнены.**
- Рекомендуется **Always-on VPN + «Блокировать подключения без VPN»**, но независимый аудит ещё не проведён.
- Тестовый сертификат QUIC действует до **07.12.2026**; оператору потребуется его обновить и переиздать доверенные профили.

**Не публикуйте** `mbox://`, PSK, ключи и необработанные логи. Переход debug → release может потребовать удаления приложения после сохранения профиля.

[Руководство Android](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4/docs/ru/ANDROID_BETA4.md) · [Полные примечания](https://github.com/RegSirius06/mbox/blob/v1.0.0-beta.4/RELEASE_NOTES.ru.md)
