# Noise в MBox

<!-- MBOX-BETA4-CURRENT-DOCS -->
> **Версионный контекст:** ниже могут быть исторические наблюдения beta.2/beta.3. В **beta 4** работают прямой QUIC/HTTP3, UDP приложений и резервный HTTPS/Cronet. Актуальные сведения: [Android](ANDROID_BETA4.md), [транспорты](BETA_4_TRANSPORTS.md), [границы безопасности](SECURITY.md). Подписанный APK уже собран и установлен, но физический аудит утечек ещё не завершён.


[English](../NOISE.md)

Suite:

```text
Noise_NKpsk0_25519_AESGCM_SHA256
```

Provisioning: `client_id` + 32-byte PSK + static server X25519 public key. `client_id` входит в prologue; обе стороны генерируют fresh ephemeral X25519. После Noise Split транспорт использует Noise CipherState напрямую. Legacy beta.1 handshake не поддерживается.

Независимый test vector сравнивается byte-for-byte.
