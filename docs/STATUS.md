# MBox release status

## 1.0.0-beta.1

Release gates completed:

- clean initialization: PASS
- duplicate initialization refusal: PASS
- destructive `init --force` with backup/reinitialize: PASS
- configuration validation: PASS
- SQLite user control plane: PASS
- server start/restart/reload: PASS
- SIGHUP authentication reload: PASS
- generated REG.RU relay deployment: PASS
- public relay probe: PASS
- cover traffic public route: PASS
- cover suppression during real sessions: PASS
- Android VPN connection through production frontend: PASS
- automatic Android reconnect: PASS
- secret/release tree review: PASS

Known beta limitations:

- TCP transport only; native UDP/QUIC tunneling is not supported.
- Cryptographic design has not received an independent security audit.
- Static server X25519 authentication does not provide strong post-compromise forward secrecy.
- Cover traffic changes the shared frontend activity profile; it does not conceal client last-mile traffic.
- Graceful CLOSE is best-effort; idle expiry remains the authoritative cleanup mechanism.
- Long-duration soak, fuzzing and heavy multi-device load testing remain future work.
