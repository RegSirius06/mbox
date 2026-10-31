# MBox

> **1.0.0-beta.1:** first beta release of the complete MBox stack, including Android VPN client, authenticated multiplexed transport, REG.RU FastCGI relay tooling, administration CLI and optional server-originated cover traffic.

MBox is an experimental encrypted multiplexed TCP transport designed to work through HTTP intermediaries that buffer complete requests/responses and therefore cannot carry an ordinary long-lived streaming tunnel.

This repository contains the server, client core, Android VPN wrapper, REG.RU FastCGI relay, installer, and the `mboxctl` administration CLI.

> **Project status:** working experimental software. The current production path has been tested end-to-end through a buffering shared-hosting FastCGI relay. It has not received an independent cryptographic/security audit.

## What is implemented

The current implementation includes:

- short complete HTTP request/response transport;
- TCP multiplexing over one logical MBox session;
- per-device `client_id` authentication;
- one independently revocable PSK per client;
- X25519 key agreement with an ephemeral client key and a static server key;
- HMAC-SHA256 authentication;
- AES-256-GCM encrypted records;
- independent UP/DOWN keys and counters;
- exact encrypted UP retry without re-encrypting/reusing a new counter;
- server-side UP deduplication and cached response replay;
- DOWN reordering;
- piggyback DOWN data in POST responses;
- standalone DOWN polling;
- JSON HTTP envelope;
- randomized record padding classes;
- randomized timing/jitter and idle polling camouflage;
- optional server-side bursty cover traffic through the public frontend while no real MBox session is active;
- exponential transport recovery and fail-fast;
- Android `VpnService` + HEV TUN-to-SOCKS integration;
- Android physical-network DNS resolution before the VPN tunnel is established;
- Android automatic reconnect supervisor;
- `mbox://` provisioning links;
- SQLite-backed user administration;
- bulk user creation/import/export;
- generated REG.RU relay configuration and deployment over SSH/SCP or FTP/FTPS.

## What is *not* implemented

Be precise about these limitations when reviewing the project:

- MBox currently transports TCP through its SOCKS endpoint; it is not a general UDP tunnel.
- Server-side cover traffic is optional and disabled by default. It changes the traffic history seen by the shared frontend, but it does **not** hide the absence of traffic on a particular client ISP/mobile link.
- The current static server X25519 key means the handshake should **not** be advertised as providing strong post-compromise forward secrecy. See `docs/SECURITY.md`.
- The project has not been independently audited.
- The REG.RU `.htaccess` / FastCGI activation mechanism is hosting-specific. MBox generates the FastCGI binary and `relay.json`; it deliberately does not blindly overwrite an existing `.htaccess`.

---

# 1. Architecture

Typical production path:

```text
Android applications
        │
        ▼
Android VpnService / TUN
        │
        ▼
hev-socks5-tunnel
        │
        ▼
MBox client SOCKS 127.0.0.1:10890
        │
        ▼
HTTPS public frontend
        │
        ▼
REG.RU shared hosting / FastCGI relay
        │
        ▼
HTTPS origin on RU VPS
        │
        ▼
MBox server 127.0.0.1:18449
        │
        ▼
SOCKS upstream 127.0.0.1:2080
        │
        ▼
exit transport / next hop
```

The MBox server does **not** require `mbox-client` to be running on the server. `mbox-client` is installed only as a useful test/debug binary.

See `docs/ARCHITECTURE.md` for protocol details.

---

# 2. Repository layout

```text
mbox/
├── install.sh                  # one entry point for a new administrator
├── server/                     # MBox HTTP server
├── client/                     # shared Go client core
├── relay/                      # FastCGI buffering relay
├── tools/
│   ├── mbox-key/               # X25519 key generation/derivation
│   ├── mbox-link/              # low-level mbox:// link tool
│   └── mboxctl/                # administrator CLI
├── android/                    # Android VpnService/HEV wrapper
│   └── tools/build-core.sh     # builds ../client, not a second protocol copy
├── deploy/
│   └── mbox-server.service
├── docs/
│   ├── ADMIN.md
│   ├── ARCHITECTURE.md
│   ├── SECURITY.md
│   ├── TESTING.md
│   └── SCHEMA.sql
└── .github/workflows/
```

The Android build intentionally uses the same Go core from `client/`. There is no separate Android copy of the transport protocol.

---

# 3. Fresh installation

## Supported automatic installer path

The installer is intended primarily for Debian/Ubuntu-like VPSes with systemd.

Clone or unpack the repository, then:

```bash
chmod +x install.sh
sudo ./install.sh
```

That is the entry point. A new user does **not** need to install `mboxctl` first.

`install.sh` will:

1. check required tools;
2. on Debian/Ubuntu, install missing Go/Python/OpenSSL/curl/OpenSSH packages;
3. save a rollback archive under `/root/mbox-preinstall-*.tar.gz`;
4. build `mbox-server`, `mbox-client`, `mbox-key`, `mbox-link` and `relay.fcgi`;
5. install `mboxctl` to `/usr/local/bin/mboxctl`;
6. install the systemd unit;
7. run either fresh initialization or legacy migration;
8. generate the origin and REG.RU deployment artifacts;
9. start the MBox service;
10. run `mboxctl doctor`.

### Fresh setup wizard

On a clean machine, `install.sh` calls `mboxctl init` automatically.

The wizard asks only for deployment-specific values that cannot safely be guessed:

- public frontend URL/hostname;
- RU origin URL;
- initial client ID.

Defaults:

```text
MBox listen:      127.0.0.1:18449
SOCKS upstream:   127.0.0.1:2080
REG relay slots:  4
initial client:   android-main
```

A cryptographically random `/edge/<token>` base path is generated automatically unless explicitly supplied.

You can also initialize non-interactively:

```bash
sudo mboxctl init \
  --public-url https://front.example.com \
  --origin-url https://origin.example.com \
  --first-user phone-main
```

If `--public-url` contains only a host, MBox appends the generated base path.

---

# 4. Migrating an existing MBox installation

If `install.sh` finds:

```text
/etc/mbox/server.json
```

but no new-style:

```text
/etc/mbox/mbox.conf
```

it runs the legacy migration instead of creating new cryptographic identity.

Manual equivalent:

```bash
sudo mboxctl migrate legacy \
  --server-json /etc/mbox/server.json
```

The migration attempts to infer the current public frontend and relay target from the previous production files. If they cannot be inferred, it asks for them interactively.

Migration preserves:

- existing X25519 server private key;
- existing base path;
- current listen/upstream addresses;
- current per-client PSKs;
- current camouflage paths/padding;
- client timing/recovery settings when the old client template is available.

It then imports users into SQLite and generates the new runtime config.

**Do not delete the rollback archive until the migrated installation has been tested.**

---

# 5. Files installed on the RU server

Human/admin configuration:

```text
/etc/mbox/mbox.conf
```

Generated runtime server configuration:

```text
/etc/mbox/server.json
```

Server keys:

```text
/etc/mbox/server.key      # secret, 0600
/etc/mbox/server.pub      # public
```

Users database:

```text
/var/lib/mbox/users.db    # secret, 0600
```

Generated REG.RU artifacts — **authoritative location**:

```text
/var/lib/mbox/regru/current/
├── relay.fcgi
├── relay.json
├── DEPLOY.txt
└── SHA256SUMS
```

Generated REG.RU archive:

```text
/var/lib/mbox/regru/mbox-regru-bundle.tar.gz
```

Generated RU-origin reverse-proxy snippet:

```text
/var/lib/mbox/origin/current/
├── Caddyfile.mbox
└── DEPLOY.txt
```

This is intentional: if an administrator asks "what exactly must be uploaded to REG.RU?", the answer is always:

```bash
sudo mboxctl relay paths
```

---

# 6. `mboxctl`: complete administrator CLI

Top-level help:

```bash
mboxctl --help
```

## Status and diagnostics

```bash
sudo mboxctl status
sudo mboxctl doctor
sudo mboxctl logs
sudo mboxctl logs -f
sudo mboxctl version
```

`doctor` validates the human config, SQLite integrity, key files, runtime config/template paths and systemd state.

## Server service

```bash
sudo mboxctl server start
sudo mboxctl server stop
sudo mboxctl server restart
sudo mboxctl server status
```

## Configuration

Show the human-readable INI config:

```bash
sudo mboxctl config show
```

Read a value:

```bash
sudo mboxctl config get server.public_url
sudo mboxctl config get server.origin_url
sudo mboxctl config get relay.max_slots
```

Change a value:

```bash
sudo mboxctl config set relay.max_slots 4
sudo mboxctl config set server.upstream 127.0.0.1:2080
```

Validate:

```bash
sudo mboxctl config validate
```

Changing general configuration regenerates runtime/relay/origin files and restarts the service where required.

`/etc/mbox/server.json` is generated state. Do not hand-edit it; changes will be overwritten by `mboxctl`.

## Synchronization

Regenerate server runtime auth/config:

```bash
sudo mboxctl sync
```

Regenerate and restart:

```bash
sudo mboxctl sync --restart
```

---

# 7. User management

## Add one user

```bash
sudo mboxctl user add alice-phone
```

The PSK is generated with a CSPRNG. It is never requested from the administrator.

## Add several named users at once

```bash
sudo mboxctl user add \
  alice-phone \
  alice-tablet \
  bob-phone \
  bob-laptop
```

The operation is one SQLite transaction: either all requested users are created or none are.

## Generate many users

```bash
sudo mboxctl user add \
  --count 100 \
  --prefix user- \
  --width 3
```

Creates:

```text
user-001
user-002
...
user-100
```

A hundred users do not create a hundred authoritative PSK files. They are rows in one SQLite database.

## CSV bulk import

Example `users.csv`:

```csv
id,label,note
alice-phone,Alice phone,primary
alice-tablet,Alice tablet,
bob-phone,Bob phone,work
```

Import:

```bash
sudo mboxctl user import users.csv
```

CSV contains metadata, not PSKs. MBox generates new PSKs itself.

## List/count users

```bash
sudo mboxctl user list
sudo mboxctl user list --enabled
sudo mboxctl user list --revoked
sudo mboxctl user count
```

## Inspect one user

```bash
sudo mboxctl user show alice-phone
```

The command deliberately does not print the PSK.

## Rename

```bash
sudo mboxctl user rename alice-phone alice-pixel
```

## Temporarily disable/enable

```bash
sudo mboxctl user disable alice-pixel
sudo mboxctl user enable alice-pixel
```

## Revoke

```bash
sudo mboxctl user revoke alice-pixel
```

Revocation blocks future MBox OPEN handshakes for that ID. Already-established sessions are not forcibly killed by the auth reload.

## Rotate a leaked PSK

```bash
sudo mboxctl user rotate alice-pixel
```

Generate and immediately print a replacement link:

```bash
sudo mboxctl user rotate alice-pixel --print-link
```

The previous PSK stops authenticating new sessions after the auth reload.

## Delete permanently

Interactive deletion:

```bash
sudo mboxctl user delete alice-pixel
```

For automation, skip the confirmation with `--yes`:

```bash
sudo mboxctl user delete alice-pixel --yes
```

Several users can be deleted atomically in one command:

```bash
sudo mboxctl user delete old-phone old-tablet test-user --yes
```

`--purge` remains accepted as a backwards-compatible alias for non-interactive deletion.

---

# 8. Client provisioning

## Print an `mbox://` link

```bash
sudo mboxctl user link alice-phone
```

**The link is secret.** It embeds that user's PSK.

## Terminal QR

If `qrencode` is installed:

```bash
sudo mboxctl user qr alice-phone
```

## Bulk export

Export selected users:

```bash
sudo mboxctl user export \
  alice-phone bob-phone \
  --output /root/provisioning
```

Export all enabled users:

```bash
sudo mboxctl user export \
  --all-enabled \
  --output /root/provisioning.tar.gz
```

If `qrencode` exists, PNG QR codes are included. The output is secret and should be removed after provisioning.

## Safely inspect a link

```bash
mboxctl link inspect profile.mbox --redact
```

Use `--redact` before pasting decoded link contents into an issue/chat.

---

# 9. SQLite database

The database is:

```text
/var/lib/mbox/users.db
```

Schema is documented in `docs/SCHEMA.sql`.

Check integrity:

```bash
sudo mboxctl db check
```

Compact database:

```bash
sudo mboxctl db vacuum
```

SQLite is the **administrative source of truth**. The data plane does not execute a SQL query for every packet.

On a user change, `mboxctl`:

```text
SQLite transaction
      │
      ▼
atomically render /etc/mbox/server.json
      │
      ▼
SIGHUP mbox-server
      │
      ▼
server atomically swaps its in-memory client_id → PSK auth map
```

Existing sessions remain alive; new handshakes use the updated map.

This avoids both per-packet DB overhead and hundreds of authoritative PSK files.

---

# 10. REG.RU relay generation

Generate/refresh files:

```bash
sudo mboxctl relay bundle
```

Show exact paths:

```bash
sudo mboxctl relay paths
```

Default files:

```text
/var/lib/mbox/regru/current/relay.fcgi
/var/lib/mbox/regru/current/relay.json
/var/lib/mbox/regru/current/DEPLOY.txt
/var/lib/mbox/regru/current/SHA256SUMS
```

The generated `relay.json` contains no client PSKs. It contains only relay routing/limits such as:

- origin HTTPS target;
- allowed MBox path prefix;
- FastCGI slot limit;
- maximum body size;
- lock-file prefix.

## Deploy to REG.RU over SSH/SCP

Example:

```bash
sudo mboxctl relay deploy \
  --ssh USER@HOST \
  --remote-dir /var/www/USER/data/www/front.example.com
```

The SSH deployment:

1. uploads into a temporary staging directory;
2. saves timestamped rollback copies of existing `relay.fcgi` and `relay.json`;
3. installs `.new` files;
4. applies executable mode to `relay.fcgi`;
5. atomically renames them into place.

Example:

```bash
sudo mboxctl relay deploy \
  --ssh hosting-user@shared-host \
  --remote-dir /var/www/hosting-user/data/www/front.example.com
```

Use the account's real web root.

## Deploy over FTPS

```bash
sudo mboxctl relay deploy \
  --ftp-host ftp.example.com \
  --ftp-user USER \
  --remote-dir /path/to/webroot
```

The password is requested interactively. It is not written into `mbox.conf`.

On shared hosting, FTP is commonly chrooted and may not expose the same absolute path as SSH. MBox therefore tries the supplied path first and then safe chroot-relative forms. For example, an ISPmanager/REG.RU SSH path like:

```text
/var/www/USER/data/www/front.example.com
```

is also tried as:

```text
/www/front.example.com
www/front.example.com
/front.example.com
front.example.com
```

The selected FTP-visible directory is printed before upload. If none exists, `mboxctl` prints the server `PWD`, visible root entries, and every attempted path instead of a Python traceback.

For old plain FTP only:

```bash
sudo mboxctl relay deploy \
  --ftp-host ftp.example.com \
  --ftp-user USER \
  --ftp-plain \
  --remote-dir /path/to/webroot
```

Prefer encrypted FTP/SSH whenever the provider supports it.

The FTP deploy tries to preserve timestamped rollback names and uses `SITE CHMOD 755 relay.fcgi` when supported by the server.

## Reachability probe

```bash
sudo mboxctl relay test
```

The unauthenticated probe normally receives 404. The purpose is to verify that the frontend route is reachable, not to authenticate a VPN session.

### `.htaccess` note

MBox does not automatically replace `.htaccess` because shared-hosting FastCGI activation differs between providers/accounts and an incorrect replacement can break an existing website. Preserve a working REG.RU FastCGI mapping and deploy only the generated `relay.fcgi` + `relay.json` unless intentionally changing routing.

---

# 11. RU origin / Caddy

Generate the current Caddy snippet:

```bash
sudo mboxctl origin caddy
```

Show its location:

```bash
sudo mboxctl origin paths
```

Generated file:

```text
/var/lib/mbox/origin/current/Caddyfile.mbox
```

It contains the current origin hostname, randomized base path, health endpoint and reverse proxy to `127.0.0.1:18449`.

It is **not automatically merged into an arbitrary existing Caddyfile** because the existing site may already contain the same host block or masquerade handlers. Review/merge it, then use standard Caddy validation/reload.

---

# 12. Camouflage currently implemented

## HTTP shape

Encrypted binary records are carried inside JSON:

```json
{"v":1,"data":"<base64>"}
```

Typical logical paths:

```text
/api/v1/session
/api/v1/events/<session>
/api/v1/sync/<session>
/api/v1/session/<session>
```

A random installation-specific `/edge/<token>` prefix is placed before them.

## Size camouflage

Default padded plaintext classes:

```text
512
1024
2048
4096
8192
16384
32768
65536
131072
262144
524288
```

Default probability of deliberately selecting one class above the minimum fitting class:

```text
20%
```

Padding bytes are random.

## Timing camouflage

Default client timing profile:

```text
idle poll:       random 45–90 s
wake delay:      random 100–220 ms
active jitter:   ±20%
```

Recovery defaults:

```text
failure timeout: 30 s
retry minimum:   300 ms
retry maximum:   5 s
```

The retry delay grows exponentially until capped.

### Client timing camouflage vs server cover traffic

MBox implements both mechanisms, for different purposes.

Client timing camouflage belongs to a real MBox session: idle polls, wake delay and active jitter make the live session less periodic.

Server cover traffic is optional (`cover.enabled = false` by default). When there is no recently active real MBox session, `mbox-server` creates irregular HTTPS activity using the configured public frontend URL. The request path is inside the already permitted MBox prefix:

```text
RU mbox-server
  → https://PUBLIC_FRONTEND/<base>/api/v1/assets/...
  → REG.RU FastCGI relay
  → RU Caddy
  → mbox-server cover resource handler
```

The engine randomly alternates between quiet, casual, active and burst profiles. Quiet periods can last many minutes; active profiles create short page-like groups of resource GETs with varied extensions, response sizes, browser-like request headers and irregular delays. Two hard limits cap request rate and requests per hour.

A successful real MBox OPEN cancels the current cover context immediately. While a real session remains recently active, no new cover requests are generated. If a client disappears without a clean CLOSE, `cover.idle_after_ms` prevents cover from starting until the real session has been inactive for the configured interval. A randomized start grace is then applied.

The cover path intentionally lives under `/api/v1/`, so an existing deployment whose relay/Caddy already route `<base>/api/v1/*` does not require a new REG.RU route or `.htaccess` rule.

This mechanism changes the activity history visible to the shared frontend. It does **not** create packets on a disconnected phone/home ISP path and therefore must not be described as hiding client-side absence of traffic.

Administration:

```bash
mboxctl cover simulate --hours 24
mboxctl cover test
mboxctl cover enable
mboxctl cover status
mboxctl cover disable
```

Configuration lives in `[cover]` in `/etc/mbox/mbox.conf`. Important defaults:

```ini
[cover]
enabled = false
path = /api/v1/assets/
idle_after_ms = 120000
start_grace_min_ms = 60000
start_grace_max_ms = 180000
max_rps = 4
max_requests_per_hour = 1200
quiet_weight = 35
casual_weight = 40
active_weight = 20
burst_weight = 5
```

Use `mboxctl config set cover.<key> <value>` for changes. Cover configuration changes restart the server; user add/revoke/rotate still use the in-process auth reload.

---

# 13. Cryptography summary

Authenticated client OPEN v2 contains:

```text
1 byte client_id length
client_id
32-byte ephemeral X25519 client public key
32-byte HMAC-SHA256
```

Authentication uses the per-client PSK.

Key derivation combines:

- X25519 shared secret;
- per-client PSK;
- session ID;
- client ID;
- client ephemeral public key;
- protocol-domain separation strings.

It derives separate key material for:

- UP AES-256-GCM;
- DOWN AES-256-GCM;
- handshake confirmation.

Encrypted records include a direction and counter. UP retry retransmits the exact same encrypted record; the server deduplicates already committed counters and can replay the cached corresponding HTTP response rather than processing MUX data twice.

See `docs/SECURITY.md` for limitations and threat-model notes.

---

# 14. Android client

The Android project is under:

```text
android/
```

The build uses the root Go client core:

```bash
cd android
./tools/build-debug.sh
```

Requirements include an Android SDK/JDK compatible with the Gradle project.

The Android wrapper provides:

- `VpnService`;
- HEV TUN → SOCKS;
- `mbox://` import;
- clipboard import;
- Connect/Disconnect UI;
- physical-network DNS resolution using Android's active `Network`;
- automatic core/TUN recovery with reconnect backoff;
- protection against routing the MBox app itself back into its own VPN.

The manifest includes `ACCESS_NETWORK_STATE` because endpoint resolution uses `ConnectivityManager.getActiveNetwork()`.

---

# 15. Backups

Create a configuration/user backup:

```bash
sudo mboxctl backup create
```

Or specify a path:

```bash
sudo mboxctl backup create --output /root/mbox-backup.tar.gz
```

Verify an archive:

```bash
sudo mboxctl backup verify /root/mbox-backup.tar.gz
```

Backups contain secrets. Keep them private.

---

# 16. Preparing a private GitHub repository

Never commit live server state.

The repository `.gitignore` excludes known key/database/profile/runtime patterns, but **always inspect the staged tree before pushing**:

```bash
git status

git diff --cached
```

Recommended local flow:

```bash
git init
git branch -M main
git add .
git status
git commit -m "Initial private MBox release"
```

With GitHub CLI installed/authenticated:

```bash
gh repo create mbox --private --source=. --remote=origin --push
```

Or create an empty private repository in GitHub's UI, then:

```bash
git remote add origin git@github.com:YOUR_USER/mbox.git
git push -u origin main
```

Before ever making the repository public, review third-party component licensing (including the bundled HEV Android AAR) and perform a secret-history scan.

---

# 17. Moving the current running deployment into this repository

For the current production system, do **not** copy `/etc/mbox`, SQLite DBs, PSKs or generated provisioning links into Git.

Copy only source/code that is not already represented by this repository.

On the RU server, after uploading this repository:

```bash
cd /root/mbox
sudo ./install.sh
```

Because `/etc/mbox/server.json` already exists, the installer performs legacy migration rather than creating a new server identity.

Then verify:

```bash
sudo mboxctl doctor
sudo mboxctl user list
sudo mboxctl relay paths
sudo mboxctl origin paths
sudo mboxctl server status
```

Generate the REG.RU files and deploy them:

```bash
sudo mboxctl relay bundle
```

then either:

```bash
sudo mboxctl relay deploy \
  --ssh USER@HOST \
  --remote-dir /path/to/webroot
```

or FTPS:

```bash
sudo mboxctl relay deploy \
  --ftp-host HOST \
  --ftp-user USER \
  --remote-dir /path/to/webroot
```

Finally test a client profile generated from the migrated DB:

```bash
sudo mboxctl user list
sudo mboxctl user link android-main
```

Do not paste the resulting link into public logs/issues; it contains a PSK.

---

# 18. Development checks

Go server:

```bash
cd server
gofmt -w *.go
go vet *.go
go build *.go
```

Go shared client:

```bash
cd client
gofmt -w *.go
go vet *.go
go build *.go
```

FastCGI relay:

```bash
cd relay
CGO_ENABLED=0 go build *.go
```

Admin CLI:

```bash
python3 -m py_compile tools/mboxctl/mboxctl.py
```

Android Go core cross-build:

```bash
cd android
./tools/build-core.sh
```

Full Android debug APK:

```bash
cd android
./tools/build-debug.sh
```

---

# 19. Suggested review focus

If you are reviewing this project, the most useful areas to challenge are:

1. custom handshake/key-derivation design and forward-secrecy properties;
2. AEAD nonce/counter and exact-retry invariants;
3. MUX ordering/deduplication under HTTP retries/reordering;
4. session cleanup/resource exhaustion;
5. traffic-analysis effectiveness of padding/timing camouflage;
6. shared-hosting FastCGI concurrency behaviour;
7. Android lifecycle and Wi-Fi/mobile transitions;
8. database/provisioning secret handling;
9. malformed-request handling and DoS resistance.

This is experimental transport software; external criticism is welcome.

## License

Unless otherwise stated, MBox is licensed under the GNU Affero General
Public License version 3 or any later version
(`AGPL-3.0-or-later`).

Third-party components retain their respective licenses. See
`THIRD_PARTY_LICENSES/`.
