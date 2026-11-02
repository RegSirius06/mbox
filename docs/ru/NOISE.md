# Noise в MBox

[English](../NOISE.md)

Suite:

```text
Noise_NKpsk0_25519_AESGCM_SHA256
```

Provisioning: `client_id` + 32-byte PSK + static server X25519 public key. `client_id` входит в prologue; обе стороны генерируют fresh ephemeral X25519. После Noise Split транспорт использует Noise CipherState напрямую. Legacy beta.1 handshake не поддерживается.

Независимый test vector сравнивается byte-for-byte.
