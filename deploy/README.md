# Production deployment

The Compose file runs mcproxy with PostgreSQL and Redis. Keep the admin panel on a separate SvelteKit 3+ server and expose only the panel's server-side proxy to operators.

The mcproxy Compose service uses a read-only root filesystem, disables privilege escalation, and provides a small non-executable `/tmp`; `/data` remains the only writable application volume. Its init wrapper retains only `CHOWN`, `SETUID`, and `SETGID` long enough to prepare `/data` and drop to the non-root application UID.

1. Create a deployment `.env` (not committed) with at least `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, and `MCPROXY_ADMIN_TOKEN`.
2. Set `MCPROXY_GATE_FORWARDING_MODE` as the backwards-compatible fallback in `.env`; configure mixed backend forwarding with each server's `forwarding_mode` field from the API or admin panel. Velocity still requires the global mode and secret.
3. Put the GeoLite2 database in the `/data` volume if GeoIP rules are enabled, or set `MCPROXY_GEOIP_PATH` to a mounted path.
4. Run `docker compose -f deploy/docker-compose.yml up -d --build`.
5. Allow TCP 25565 publicly. Keep port 8080 on an internal network or behind the SvelteKit server/reverse proxy.

Before an application upgrade, create a database backup and apply the schema explicitly:

```sh
./deploy/backup.sh ./backups
docker compose -f deploy/docker-compose.yml run --rm mcproxy migrate
docker compose -f deploy/docker-compose.yml up -d --build mcproxy
curl -fsS http://127.0.0.1:8080/ready
```

The migration is additive and repeatable. Normal application startup also runs the same Ent schema reconciliation as a safety net.

The sample Compose stack defaults `POSTGRES_SSLMODE=disable` because its PostgreSQL service is an internal local container. For production across hosts or any untrusted network, set `POSTGRES_SSLMODE=require` or `verify-full` and provision the corresponding PostgreSQL CA/certificate validation configuration before deployment.

The admin API limits failed authentication attempts to 20 per source IP per minute. Keep the API behind a private network or trusted reverse proxy, and rotate `MCPROXY_ADMIN_TOKEN` if it may have been exposed.

Leave `MCPROXY_TRUST_PROXY_HEADERS` disabled unless the front proxy strips incoming `True-Client-IP`, `X-Real-IP`, and `X-Forwarded-For` headers and writes trusted values.

The panel server should call `http://mcproxy:8080/api/v1` with `Authorization: Bearer <token>` from server-side code. Do not store the token in browser local storage or expose it in client JavaScript. If direct browser API calls are unavoidable, set `MCPROXY_ADMIN_ALLOWED_ORIGINS` to an exact HTTPS origin.

Redis authentication is required by the Compose stack. For an external Redis deployment, set `MCPROXY_REDIS_PASSWORD` and enable `MCPROXY_REDIS_TLS=true` when the endpoint requires TLS.

When a secret manager mounts files into the container, use `MCPROXY_ADMIN_TOKEN_FILE`, `MCPROXY_POSTGRES_DSN_FILE`, `MCPROXY_REDIS_PASSWORD_FILE`, `MCPROXY_GATE_VELOCITY_SECRET_FILE`, or `MCPROXY_GATE_BUNGEGUARD_SECRET_FILE`. The corresponding direct environment variable wins if both are present.

Before production, add TLS termination, firewall the Minecraft backends so only mcproxy can reach them, and configure Prometheus to scrape `/metrics` from an internal network.

Prometheus examples are provided in [observability/prometheus.yml](observability/prometheus.yml) and [observability/alerts.yml](observability/alerts.yml). Mount them into the Prometheus service and verify the `mcproxy` target is `UP` before relying on the alerts.

Kubernetes templates for mcproxy and the separate admin panel are in [kubernetes](kubernetes). They expect external PostgreSQL/Redis services and pre-created Secrets; replace image tags, hostnames, TLS origins, resource limits, and ingress settings for the target cluster.

Create a PostgreSQL backup with `./deploy/backup.sh [backup-directory]`. Store the resulting custom-format dump outside the host and periodically test restoration on a separate PostgreSQL instance. The checked-in restore helper requires an explicit confirmation token:

```sh
RESTORE_CONFIRM=mcproxy-restore ./deploy/restore.sh \
  backups/mcproxy-postgres-<timestamp>.dump
```

Do not run the restore helper against production without a maintenance window and a fresh pre-restore backup. Set `POSTGRES_USER`, `POSTGRES_DB`, and `COMPOSE_FILE` when the target differs from the Compose defaults.

The isolated recovery rehearsal can be run with `make postgres-restore-smoke`. It starts a disposable PostgreSQL Compose project, applies the current migration, inserts a fixture, creates a custom-format dump, restores it into a second database, and verifies the fixture. It removes the temporary containers and volumes on exit.
