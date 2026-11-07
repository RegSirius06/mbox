# MBox Android QUIC — изолированная проверка (этап 2)

Этот патч добавляет в **debug APK** переключатель **HTTPS ↔ QUIC lab**.
Оба транспорта используют существующий Noise MBox и один и тот же origin/sing-box.
**Пока это ручной выбор между двумя транспортами, а не автоматическая балансировка по потокам.**
QUIC несёт HTTP/3 streams; QUIC DATAGRAM и многоканальный диспетчер ещё не реализованы.

## 0. Состояние VPS

На RU VPS должен работать `mbox-native-quic serve` (UDP/7443), как в предыдущем этапе.
Нынешние `/etc/mbox/quic/server.crt` / `server.key` подходят. Не создавать новые ключи.

## 1. Подготовить debug APK на Arch

```bash
cd ~/src/mbox-main
mkdir -p android/app/src/debug/assets
scp root@84.54.44.148:/etc/mbox/quic/server.crt /tmp/mbox-quic.crt
./android/tools/configure-quic-lab.sh https://84.54.44.148:7443 /tmp/mbox-quic.crt
./android/tools/build-debug.sh
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
```

`configure-quic-lab.sh` встраивает **только публичный сертификат** и адрес VPS в
debug APK. Не копировать `server.key` или HMAC-ключ. Эти файлы игнорируются Git.
Go 1.26+ обязателен для `native-quic`; Go 1.27.3 на Arch подходит.

## 2. Проверить

1. Отключить MBox в приложении.
2. Кнопкой `Transport: HTTPS` выбрать `QUIC laboratory`.
3. Подключить MBox и проверить Telegram, YouTube, UDP Lab STUN.
4. Для сравнения снова отключиться и переключиться обратно на `HTTPS`.

Выбор сохраняется локально. Профиль/подписку повторно импортировать не нужно.
На время эксперимента желательно Android Always-on VPN + «Блокировать подключения без VPN».

## 3. Если QUIC не подключается

```bash
adb logcat -d -s MBox:I | tail -n 120
```

Клиент **не делает прямого fallback** на сеть провайдера. QUIC-сокеты,
передающие только зашифрованный транспорт MBox, привязываются к
физическому Android Network до отправки первого пакета (FD через SCM_RIGHTS).
Остальные приложения продолжают работать через TUN/HEV.

Если QUIC потерян, `MBox` остаётся fail-closed: TUN не снимается. Для возврата
к HTTPS надо отключиться, выбрать его и снова подключиться.

## 4. Ограничения

- Пока отсутствует автоматическое распределение по HTTP3/HTTPS для отдельных
  пользовательских потоков и QUIC DATAGRAM. Не называем это выполненным.
- Полную Android Gradle-сборку и проверку физического устройства нужно выполнить на Arch.
- Используется текущий самоподписанный TLS-сертификат с IP SAN: при истечении срока
  или смене сертификата требуется пересобрать лабораторный APK с новым публичным cert.
- Не публиковать эту debug сборку как production.
