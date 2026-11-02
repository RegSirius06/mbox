# MBox beta.2 cryptography: Noise-only

[Русская версия](ru/NOISE.md)

MBox 1.0.0-beta.2 removes the previous custom handshake and custom transport-key derivation. There is no legacy/downgrade path in the beta.2 client or server.

## Suite and wire generation

The handshake suite is exactly:

`Noise_NKpsk0_25519_AESGCM_SHA256`

The outer MBox wire generation is called `MBox/3` because it is incompatible with the previous beta wire format. Release number `1.0.0-beta.2` and wire generation `MBox/3` are separate version numbers.

Existing provisioning material is reused:

- client ID;
- a 32-byte per-client PSK;
- the server's static X25519 public key.

The client ID is carried in the HTTPS-protected OPEN envelope so the server can choose the correct PSK. It is also mixed into the Noise prologue (`MBOX3-NOISE\\0 || client_id`), so changing the ID changes the authenticated handshake transcript.

Noise NK uses the responder static key as a pre-message. `psk0` mixes the existing 256-bit client PSK before the first ephemeral key operation. Both sides generate fresh ephemeral X25519 keys for each session; the `ee` operation in the second Noise message gives forward secrecy against later disclosure of the server static key/PSK, assuming the ephemeral private keys have been destroyed.

The server returns the MBox session ID as the encrypted/authenticated payload of Noise handshake message 2. The client accepts a session only after that payload authenticates successfully.

beta.2.1 Android clients may also include a stable random `device_id` in the authenticated OPEN payload. This identifier is not a cryptographic key and is not placed in the public URL. The server uses it only after successful authentication to supersede stale sessions from the same `client_id + device_id`; clients that omit it keep the legacy per-client session-cap behavior. The Noise suite and transport record format remain unchanged.

## Transport encryption

After Noise `Split`, MBox uses the two Noise transport cipher states directly:

- initiator -> responder for UP;
- responder -> initiator for DOWN.

MBox does not derive a second set of home-grown AES keys after Noise.

A logical MBox HTTP record can contain multiple Noise transport messages. Each Noise ciphertext is at most 65535 bytes as required by the Noise framing limit. This allows an HTTP transaction to remain larger than one Noise message without weakening the existing HTTP retry/replay machinery.

The logical MBox record header authenticates, as AEAD associated data:

- direction;
- logical record counter;
- first Noise nonce;
- chunk index/length;
- session ID.

The existing DOWN request sequence/ACK/exact replay cache remains outside Noise. A lost HTTP response is retried by returning the already-committed byte-identical ciphertext; it is never re-encrypted with the same nonce.

## No legacy fallback

beta.2 intentionally contains no parser or negotiation for the previous custom handshake/record layer. A beta.1/hardened client cannot connect to a beta.2 server and a beta.2 client cannot downgrade to a beta.1 handshake.

Update client and server together. During a staged deployment, expect the old Android client to stop connecting as soon as the server is upgraded.

A deprecated `psk_file` field may remain in an old administrative/runtime configuration during migration, but beta.2's server cryptographic code does not consume it as a legacy server-wide credential and there is no legacy handshake path.

## Independent vector validation

The vendored minimal Noise implementation is deliberately restricted to one suite. Tests reproduce an independent `Noise_NKpsk0_25519_AESGCM_SHA256` vector byte-for-byte: both handshake messages, handshake hash and transport ciphertexts.

The implementation is derived from the BSD-licensed `github.com/flynn/noise` state machine/HKDF construction and uses Go's standard-library X25519/AES-GCM/SHA-256 primitives. The corresponding license is retained under `common/noise/LICENSE`.

## Security status

Replacing the custom handshake greatly reduces the amount of original cryptographic protocol design in MBox, but it is not an external audit of the whole product. MBox-specific framing, replay/ACK logic, counters, flow control, provisioning and the Noise integration still require independent review before a production/security-audited claim.
