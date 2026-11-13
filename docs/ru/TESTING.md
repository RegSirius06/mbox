# MBox beta.4.1 — воспроизводимое тестирование и доказательства

[English](../TESTING.md) · [Матрица утечек](ANDROID_FAIL_CLOSED_TEST_PLAN.md) · [Статус](BETA4_1_STATUS.md)

## Автоматические проверки на точном коммите

```bash
cd ~/src/mbox-main
git diff --check
GO111MODULE=off go test -race ./client ./server
GO111MODULE=off go vet ./client ./server
(cd native-quic && go vet ./... && go test -race -count=1 ./...)
python3 -m pytest -q tests
python3 tools/check-source-layout.py
./tools/secret-scan.sh
(cd android && ./tools/build-debug.sh && ./gradlew assembleRelease)
python3 tools/check-unsigned-apk.py android/app/build/outputs/apk/release/app-release-unsigned.apk
```

Нужен **зелёный финальный прогон** с SHA коммита и количеством тестов. В полученном первоначальном логе Go client/server и native QUIC прошли, Python — 114 passed / 1 failed из-за проверки пробелов. Исправление теста выдано, но итоговый зелёный лог в этой беседе отдельно не приложен.

## Финальный APK с production-подписью

Использовать **существующий keystore**, не debug и не новый ключ, `tools/build-release-apk.sh`, затем `tools/check-production-apk.sh`. Проверить `versionCode=8`, non-debuggable, application ID, обе ARM64 библиотеки/исполняемых файла и отсутствие лабораторных assets. Посчитать **новый** SHA-256 именно подписанного APK и опубликовать точное значение. Обновить beta.4 через `adb install -r` (при совпадении подписи) и повторить smoke/failure-тесты. В публичный релиз не класть debug или unsigned сборки.

## Натурные проверки

- Ручные QUIC/HTTPS и Auto с работающим QUIC, заблокированным UDP при доступном HTTPS и отказом обоих путей; восстановление, Stop посреди восстановления.
- Простой с экраном off отдельно от принудительного Doze; пробуждение, Wi-Fi→LTE→Wi-Fi, DNS/IPv6, TCP/UDP, долгий upload/download и потери.
- Отдельная остановка QUIC gateway, Go core и HEV; имитация живого зависшего процесса; проверка ограниченного backoff.
- Always-on включён/выключен; Android Lockdown отдельно; захват трафика с **физического интерфейса** или роутера по IPv4, IPv6, DNS, TCP и UDP до/во время/после аварии VPN. После полной смерти процесса системную блокировку обеспечивает Lockdown.
- RTT/jitter/loss, батарея, частота heartbeat; сравнение TLS/HTTP fingerprint браузера и MBox на контролируемом endpoint.

**По сообщению разработчика:** 5 основных сценариев подключения пройдены. **Не подтверждено:** управляемые остановки процессов, независимый packet capture, окончательная приёмка signed APK и долгий soak. Не проставлять галочки за невыполненные испытания.
