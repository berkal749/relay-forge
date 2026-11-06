# Operations Runbook

This document covers the beta deployment: bootstrap, access control, quotas,
TLS, monitoring, backups, and recovery.

## Deployment profiles

| Profile | Command | Use |
| --- | --- | --- |
| Local development | `docker compose up -d postgres` + `make run-api` + `make web-dev` | Day-to-day work |
| Private beta | `docker compose -f compose.yaml -f compose.production.yaml up -d --build` | Trusted users behind TLS |
| Public beta | Same, after the go/no-go checklist below | External signups by invite |

The production overlay:

- serves the dashboard and terminates TLS with Caddy,
- publishes only ports 80/443,
- keeps PostgreSQL and the API on an internal Docker network,
- runs API, supervisor, and workers as non-root with resource limits; Caddy starts as root only to bind low ports and fix volume ownership, then drops to the `caddy` user.

## Required production configuration

Generate independent secrets:

```sh
openssl rand -hex 32   # API_KEY_PEPPER
openssl rand -hex 32   # WORKER_TOKEN_PEPPER
openssl rand -hex 24   # POSTGRES_PASSWORD
```

Set at minimum:

```sh
APP_ENV=production
POSTGRES_PASSWORD=<strong secret>
API_KEY_PEPPER=<32+ random characters>
WORKER_TOKEN_PEPPER=<different 32+ random characters>
REGISTRATION_MODE=invite
TRUST_PROXY_HEADERS=true
BOOTSTRAP_ADMIN_EMAIL=you@example.com
SITE_ADDRESS=relayforge.example.com
RELAYFORGE_API_URL=https://relayforge.example.com
MAX_PROJECTS_PER_USER=5
MAX_WORKER_POOLS_PER_PROJECT=10
MAX_ACTIVE_POOLS_PER_PROJECT=5
MAX_WORKER_CONCURRENCY_PER_PROJECT=50
MAX_QUEUED_JOBS_PER_PROJECT=100000
```

The API refuses to start in production when a secret is missing, the two peppers
are equal, `REGISTRATION_MODE=open`, or `TRUST_PROXY_HEADERS` is not explicit.

## Bootstrap the first administrator

1. Set `BOOTSTRAP_ADMIN_EMAIL` to the first operator's email.
2. Start the stack and register that email. The account is created with the
   `admin` role even when `REGISTRATION_MODE=invite`.
3. Open **Invites** in the dashboard and create an invite for the next user.
4. Remove `BOOTSTRAP_ADMIN_EMAIL` after the first admin exists.

Invites are single-use, hashed at rest, expire after `INVITE_TTL` (default 7
days), and can optionally be bound to an email address. Revoking an invite
deletes it immediately.

## Quotas

Limits are enforced on project creation, worker-pool creation, pool updates, and
job submission. `0` disables a limit. The supervisor also applies a defensive
active-pool and concurrency cap, so a direct database edit cannot over-spawn
workers. The project page shows current usage against each limit.

| Limit | Default | Enforced in |
| --- | --- | --- |
| `MAX_PROJECTS_PER_USER` | 5 | `project.Service.Create` |
| `MAX_WORKER_POOLS_PER_PROJECT` | 10 | `workerpool.Service.Create` |
| `MAX_ACTIVE_POOLS_PER_PROJECT` | 5 | create/update + supervisor |
| `MAX_WORKER_CONCURRENCY_PER_PROJECT` | 50 | create/update + supervisor |
| `MAX_QUEUED_JOBS_PER_PROJECT` | 100000 | `job.Service.Submit` |
| `MAX_SCHEDULES_PER_PROJECT` | 50 | `schedule.Service.Create` |

## Built-in actions and credentials

Workers include `http.request`, `delay.run`, `notify.email`, `notify.slack`,
`notify.discord`, `notify.telegram`, and `notify.sms` handlers. Credentials are
operator-wide and belong in the worker environment or `WORKER_SECRETS_FILE`:

```sh
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=...
SMTP_PASSWORD=...
SMTP_FROM="RelayForge <jobs@example.com>"
SMTP_TLS=starttls            # starttls | tls | none
SLACK_WEBHOOK_URL=
DISCORD_WEBHOOK_URL=
TELEGRAM_BOT_TOKEN=
TWILIO_ACCOUNT_SID=
TWILIO_AUTH_TOKEN=
TWILIO_FROM_NUMBER=
```

They never enter the database. Customers select destinations and content in job
payloads; per-job Slack/Discord webhooks and Twilio `from` values override the
defaults.

Outbound HTTP is restricted by `WORKER_EGRESS_ALLOWLIST` (exact hosts or
`*.example.com`). Loopback, private, link-local, and multicast destinations are
refused unless `WORKER_ALLOW_PRIVATE_EGRESS=true`; the check runs when the
connection is dialled, so redirects and DNS rebinding are covered. Use the
private-egress flag only for intentionally internal webhooks.

`5xx`, `408`, `425`, `429`, and network errors retry under the job policy. Most
`4xx` responses and invalid payloads are terminal, so a bad request does not
burn through attempts.

## Schedules

Cron schedules live in the `schedules` table and are evaluated by the API every
`API_SCHEDULER_INTERVAL` (default 15s). They are independent of worker
availability: a due schedule submits a job, then advances `next_run_at`.
`MAX_SCHEDULES_PER_PROJECT` (default 50) caps them per project, and the project
dashboard has a schedules panel.

Cron uses five fields (minute, hour, day of month, month, day of week) or
descriptors such as `@daily`, evaluated in the schedule's IANA timezone.
Submissions use `schedule:<id>:<run-time>` as the idempotency key, and advancing
is optimistic, so multiple API replicas can run the scheduler safely. After
scheduler downtime one job is submitted per schedule on the next tick; there is
no missed-run burst.

## TLS and networking

Caddy obtains a certificate automatically for `SITE_ADDRESS`. It sets HSTS,
`X-Content-Type-Options`, `X-Frame-Options`, a restrictive CSP, and related
headers. Only Caddy is reachable from outside; PostgreSQL has no host port.
Remote workers reach the API over the internal network, and external workers
use the public HTTPS URL.

`TRUST_PROXY_HEADERS=true` is only safe because Caddy is the sole ingress. If
you replace Caddy, keep this invariant or set it to `false`. For client
certificates, set `WORKER_MTLS_HEADER` to the header injected by your proxy after
mTLS termination.

## Monitoring

The API exposes Prometheus metrics on `METRICS_ADDR` (default `:9090`), which is
internal-only. `/healthz` is liveness; `/readyz` pings PostgreSQL.

Metrics include HTTP request/latency by route, claims, successes, failures,
retries, queue depth per project, online workers per project, reaped leases, and
outbox backlog. `deployments/prometheus/alerts.yml` contains starter alerts, and
`deployments/prometheus/prometheus.yml` scrapes `api:9090`.

## Backups and restore

Create a backup:

```sh
BACKUP_DIR=/var/backups/relayforge RETENTION_DAYS=14 ./scripts/backup.sh
```

The script writes a custom-format `pg_dump`, rejects dumps under 1 KiB, and
deletes backups older than the retention window. Schedule it daily with cron or
a systemd timer and copy the directory off-host.

Restore drill:

```sh
docker compose stop api supervisor
docker compose exec -T postgres dropdb -U relayforge --if-exists relayforge
docker compose exec -T postgres createdb -U relayforge relayforge
docker compose exec -T postgres pg_restore -U relayforge -d relayforge --no-owner --no-privileges < /var/backups/relayforge/relayforge-YYYYMMDDTHHMMSSZ.dump
docker compose start api supervisor
curl -f https://relayforge.example.com/api/readyz
```

Run this drill before the public beta and record the result.

## Workers and recovery

- Supervised workers run in `remote` mode and need no database access.
- Each claim records a lease of `job timeout + WORKER_LEASE_GRACE`.
- Heartbeats renew leases. The API reaper fails expired executions with
  `worker lease expired` and retries under the pool policy until attempts are
  exhausted.
- Stale workers are marked offline after `WORKER_STALE_AFTER`.
- Rotate a worker token in the dashboard. The old token keeps working for
  `TOKEN_ROTATION_GRACE` (default 24h). Replace the worker's cached token file
  or restart it with a fresh enrollment token before the grace period ends.
- To re-enroll a worker, delete its token file and restart with a fresh
  `WORKER_ENROLLMENT_TOKEN`.

## Go/no-go checklist

Private beta can start after the first four items. Public beta requires all of
them.

- [ ] Strong, independent peppers set; production config starts cleanly.
- [ ] TLS active and HTTP redirects to HTTPS.
- [ ] PostgreSQL has no public port.
- [ ] `REGISTRATION_MODE=invite` and the bootstrap admin exists.
- [ ] Quotas configured for the expected beta size.
- [ ] CI green on `main`.
- [ ] Integration and failure-recovery tests pass.
- [ ] Metrics scraped and alerts routed to a real channel.
- [ ] Daily backups scheduled and a restore drill completed.
- [ ] Containers run as non-root with CPU/memory limits.
- [ ] `docs/operations.md` reviewed by the on-call operator.
