# Проверки MBox beta 4

[English](../TESTING.md) · [План испытаний утечек](../ANDROID_FAIL_CLOSED_TEST_PLAN.md) · [Чек-лист релиза](../../RELEASE_CHECKLIST.ru.md)

### Автоматические на Arch

```bash
cd ~/src/mbox-main
git diff --check
(cd client && GO111MODULE=off go test -race ./... && GO111MODULE=off go vet ./...)
(cd server && GO111MODULE=off go test -race ./... && GO111MODULE=off go vet ./...)
(cd native-quic && go test -race ./... && go vet ./...)
python3 -m pytest -q tests
./tools/secret-scan.sh
```

Разработчик получил **110 пройденных Python-тестов**, собрал и установил подписанный release APK. Это не независимый аудит; перед тегированием проверки нужно повторить на точном коммите.

### Ручная приёмка подписанного APK

Импорт приватного профиля, ручной QUIC на Wi-Fi, ручной HTTPS, Auto при ограничении мобильного интернета, смена Wi-Fi↔LTE, DNS/HTTPS/Telegram, UDP/STUN при возможности, Always-on/Lockdown. Перед публикацией `adb logcat` удаляйте адреса и секреты. Для доказательства отсутствия утечек необходим packet capture на физической сети.

### Что действительно проверялось

На debug: Wi-Fi QUIC, МТС/МегаФон HTTPS, Auto и переподключения, Telegram. На подписанном APK подтверждены установка и версия `1.0.0-beta.4` (`versionCode=7`). Результаты полного транспортного теста именно release APK не зафиксированы, Tele2 и длительный soak ещё не завершены.
