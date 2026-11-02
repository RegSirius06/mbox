# MBox Android

Русский | [English](README.md)

Android-обёртка вокруг общего Go core MBox и HEV tun2socks.

## Схема

```text
Android apps -> VpnService/TUN -> HEV -> MBox SOCKS
    -> authenticated localhost bridge -> Chromium Cronet HTTPS
    -> shared-host frontend -> MBox server
```

В normal `cronet` mode внешний DNS/TCP/TLS/ALPN/HTTP2 делает bundled Chromium Cronet. Go отвечает за Noise/MUX/replay/ACK. В `go` mode остаётся явный A/AAAA fallback для диагностики.

Underlying network monitor подписан только на `INTERNET + NOT_VPN`, поэтому собственный MBox VPN не вызывает self-reconnect. При полном исчезновении связи supervisor ждёт `UNDERLYING_AVAILABLE`, а не долбит сеть вслепую.

## Сборка на Arch

```bash
sudo pacman -S --needed jdk17-openjdk
export JAVA_HOME=/usr/lib/jvm/java-17-openjdk
export PATH="$JAVA_HOME/bin:$PATH"
hash -r

./tools/build-debug.sh
adb install -r app/build/outputs/apk/debug/app-debug.apk
```

Текущий APK ARM64-only. HEV AAR проверяется по SHA-256.

## Provisioning

Профиль содержит `client_id`, PSK и public key сервера. Перед заменой приложение показывает endpoint/client ID и SHA-256 fingerprint public key. Проверяйте fingerprint по независимому доверенному каналу.

Новые HTTPS App Links несут секретный профиль во fragment `#profile=...`, чтобы он не попадал в обычный HTTP request/query log. Старый query-формат всё ещё импортируется.

## Handover и жёсткий обрыв

Реальным тестом проверены Wi-Fi/mobile смена и airplane-mode:

```text
UNDERLYING_LOST
WAIT_UNDERLYING
...
UNDERLYING_AVAILABLE
RECONNECT reason=underlying_network immediate=true
HTTP_OK h2
NOISE_OK
TUNNEL_ESTABLISHED
VPN STARTED
```

Установка приложения хранит стабильный случайный `device_id`. Он передаётся внутри аутентифицированного Noise OPEN, поэтому новая сессия supersede'ит stale-сессию этого же телефона и не ждёт пяти минут server cleanup.

## Fingerprint

Cronet убирает очевидный Go TLS/HTTP fingerprint и даёт цельный Chromium-derived app network stack. Это не точная эмуляция Chrome. QUIC пока выключен; HTTP/2 включён. Для сравнения:

```bash
sudo mboxctl user link USER --android-http-stack cronet --network-debug
sudo mboxctl user link USER --android-http-stack go --network-debug
```

## Ограничения

- TCP transport; нативный UDP tunnel не реализован;
- ARM64-only;
- один активный профиль;
- Always-on/lockdown пока не поддерживается безопасно: app UID исключён из собственного TUN для anti-loop.
