# Ресурсы и лимиты

[English](../RESOURCES.md)

Дефолт для небольшого VPS:

```text
max_sessions_global = 128
default_max_sessions_per_client = 4
max_streams_per_session = 128
max_total_streams = 1024
max_stream_opens_per_second = 32
max_session_opens_per_minute = 180
max_concurrent_handshakes = 32
max_concurrent_http_requests = 64
max_handshake_attempts_per_second = 64
max_down_queue_bytes = 4 MiB
max_down_replay_bytes = 8 MiB
global_queued_bytes = 64 MiB
```

Полевой stress test Telegram/Instagram/YouTube дал пик 64 stream. После idle число потоков падало, leak не подтвердился. Поэтому 128 оставляет примерно двукратный запас.

Мониторинг:

```bash
mboxctl resources
```

Не поднимайте лимиты только чтобы скрыть lifecycle bug: сначала смотрите, освобождаются ли stream/session.
