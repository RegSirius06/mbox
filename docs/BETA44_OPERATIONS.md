**Current beta.4.4 production context:** origin rekey, gateway and DATAGRAM Unix socket/ACL are separate deployment components. Country `egress_id` requires an origin-allowlisted loopback SOCKS5 and `egress.json` readable by `mbox` (`root:mbox 0640`). Admin changes do not upgrade the origin executable. `bash install.sh --check` is read-only; end-to-end upgrade/rollback requires staging. REG.RU CPU/worker quotas remain a high-priority operational risk.

# beta.4.4: on-demand operations diagnostics

These commands are read-only except for the bounded HTTP requests made by `relay bench`. They do not restart services, alter configuration, kill workers, or expose a public admin listener.

## REG.RU snapshot

Run on the origin VPS as root:

```sh
mboxctl relay stats
```

The command uses the saved SSH deployment profile and takes one snapshot on REG.RU. It reports the configured slot limit, existing slot-lock files, active `flock` locks when `/proc/locks` is readable, and `relay.fcgi` worker PID, elapsed time, cumulative CPU time and RSS. It does not poll continuously. `CPU_TIME` is cumulative process time, not an instantaneous CPU percentage. If the hosting provider hides `/proc/locks` or process arguments, the corresponding values are reported as unknown/unavailable rather than guessed.

## Bounded relay health test

```sh
mboxctl relay bench --confirm --requests 40 --concurrency 2
```

This sends a finite number of `GET` requests to the configured public `/health` endpoint. That route is explicitly allowlisted by the relay and does not create a user session or send a profile, PSK, token, or user data. The request count is limited to 500, submission rate to 20 requests/second (default 4), concurrency to twice the configured slot count (maximum 16), and each request has a bounded timeout. The command reports HTTP status counts, throughput and p50/p95/p99 latency. It is a controlled acceptance test, not a DDoS tool and not a substitute for real multi-device VPN traffic.

Take `mboxctl relay stats` before and after the test. Compare active slots, worker RSS and cumulative CPU time. ISPmanager's provider-side CPU graphs remain the authoritative source for shared-host CPU percentage; this command cannot infer it.

## Web Admin backend

```sh
mboxctl admin status
```

This checks `mbox-admin.service`, the private Unix socket, and a local HTTP 200 response over that socket. It does not open a TCP port or change the SSH-only access policy. From the workstation, create the existing SSH tunnel:

```sh
ssh -N -L 8765:/run/mbox-admin/admin.sock root@<VPS_HOST>
```

Then open `http://127.0.0.1:8765/`. A healthy local backend does not by itself prove that the SSH tunnel or browser is configured correctly.

## FastCGI deadline

The 45-second watchdog now covers the complete application handler, not just the upstream proxy. This includes request-body reads, validation, slot admission and proxy work. The 40-second read-inactivity deadline still covers silence before/while FastCGI parses the request. A watchdog exit can interrupt other requests handled by the same worker; verify the REG.RU worker model and keep valid requests below the 45-second cap. No scheduled kill or worker-recycling cron is installed.
