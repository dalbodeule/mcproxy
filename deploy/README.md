# Production deployment

The Compose file runs mcproxy with PostgreSQL and Redis. Keep the admin panel on a separate SvelteKit 3+ server and expose only the panel's server-side proxy to operators.

The mcproxy Compose service uses a read-only root filesystem, disables privilege escalation, and provides a small non-executable `/tmp`; `/data` remains the only writable application volume. The image declares `USER nonroot:nonroot` and the mounted `/data` volume must be writable by UID/GID 65532.

1. Create a deployment `.env` (not committed) with at least `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, and `MCPROXY_ADMIN_TOKEN`.
2. Set `MCPROXY_GATE_FORWARDING_MODE` to `none`, `legacy`, or `velocity` as the backwards-compatible fallback in `.env`; configure mixed backend forwarding with each server's `forwarding_mode` field from the API or admin panel. BungeeGuard is only valid per server and must not be set as the global mode. Velocity still requires the global mode and secret.
3. Put the GeoLite2 City and, when ASN rules are used, GeoLite2 ASN databases in the `/data` volume, or set `MCPROXY_GEOIP_PATH` and `MCPROXY_GEOIP_ASN_PATH` to mounted paths.
4. Run `docker compose -f deploy/docker-compose.yml up -d --build`.
5. Allow TCP 25565 publicly. Keep port 8080 on an internal network or behind the SvelteKit server/reverse proxy.

Before rollout, run `make preflight` as a local smoke test and run `./deploy/preflight.sh` in the deployment environment with the real secret environment variables or their `*_FILE` mounts. It checks that the admin token and Redis password are not missing or known placeholders, that PostgreSQL has a DSN, that Redis-enabled production deployments explicitly use `MCPROXY_REDIS_FAIL_CLOSED=true`, that Velocity mode has a secret, and that the global forwarding mode is not the invalid `bungeeguard` value. Set `MCPROXY_REQUIRE_POSTGRES_TLS=true` for external PostgreSQL so the DSN must explicitly use `sslmode=require`, `verify-ca`, or `verify-full`; set `MCPROXY_REQUIRE_REDIS_TLS=true` for external Redis so `MCPROXY_REDIS_TLS=true` is required. It also rejects the removed global BungeeGuard secret so stale deployments fail clearly instead of silently ignoring it. BungeeGuard remains a per-server `forwarding_mode` setting; it never prints secret values.

For a repeatable pre-release gate, run `make release-check`; it covers application tests, race detection, panel build, deployment manifest validation, and both preflight smoke tests. When Docker is available, run `make panel-rehearsal` to execute the panel login, membership, legacy migration, security, and backup/restore rehearsals together.

For the complete local operations rehearsal, run `make ops-rehearsal`. It runs the release gate plus the real Docker Gate protocol 777 check, PostgreSQL/Redis fail-closed Compose check, PostgreSQL backup/restore check, and the full admin-panel rehearsal. It uses disposable smoke resources and cleans them up on completion.

Build release images with `MCPROXY_VERSION`, `MCPROXY_COMMIT`, and `MCPROXY_BUILD_TIME` Docker build arguments (the CI workflow supplies the Git ref, SHA, and UTC time). Confirm the deployed values through `mcproxy_build_info` in `/metrics` or the admin panel runtime card.

Run `./deploy/panel-preflight.sh` for the separate SvelteKit panel as well. It validates the panel's server-only mcproxy token and session secret, API URL, persistent SQLite path, and (with `PANEL_PREFLIGHT_REQUIRE_HTTPS=true`) the TLS Origin before rollout. Use `make panel-preflight-smoke` to verify the guard locally.

Before an application upgrade, create a database backup and apply the schema explicitly:

```sh
./deploy/backup.sh ./backups
docker compose -f deploy/docker-compose.yml run --rm mcproxy migrate
docker compose -f deploy/docker-compose.yml up -d --build mcproxy
curl -fsS http://127.0.0.1:8080/ready
```

The migration is additive and repeatable. Normal application startup also runs the same Ent schema reconciliation as a safety net.

The sample Compose stack defaults `POSTGRES_SSLMODE=disable` because its PostgreSQL service is an internal local container. For production across hosts or any untrusted network, set `POSTGRES_SSLMODE=require` or `verify-full` and provision the corresponding PostgreSQL CA/certificate validation configuration before deployment.

The admin API limits failed authentication attempts to 20 per source IP per minute. The panel additionally limits failed logins per source address and normalized username. Keep both services behind a private network or trusted reverse proxy, and rotate `MCPROXY_ADMIN_TOKEN` if it may have been exposed.

Leave `MCPROXY_TRUST_PROXY_HEADERS` disabled unless the front proxy strips incoming `True-Client-IP`, `X-Real-IP`, and `X-Forwarded-For` headers and writes trusted values. When enabled, also set `MCPROXY_TRUSTED_PROXY_CIDRS` to the exact L7 proxy networks; forwarded headers from peers outside those CIDRs are ignored.

If `MCPROXY_GATE_PROXY_PROTOCOL=true` is enabled for client connections, set `MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES` to the exact L4 proxy or load-balancer IPs/CIDRs. The application rejects startup when the list is missing or contains an invalid network, preventing untrusted clients from injecting PROXY protocol source addresses.

The panel server should call `http://mcproxy:8080/api/v1` with `Authorization: Bearer <token>` from server-side code. Do not store the token in browser local storage or expose it in client JavaScript. If direct browser API calls are unavoidable, set `MCPROXY_ADMIN_ALLOWED_ORIGINS` to an exact HTTPS origin.

The mcproxy API and the panel's server-side API proxy reject request bodies larger than 1 MiB. Keep policy and rule payloads compact; larger requests return HTTP 413 before reaching the upstream service.

Redis authentication is required by the Compose stack. The stack defaults to `MCPROXY_REDIS_FAIL_CLOSED=true`, so Redis outages reject new policy evaluations instead of silently weakening distributed protection; set it to `false` only when availability during an outage is the deliberate choice. For an external Redis deployment, set `MCPROXY_REDIS_PASSWORD` and enable `MCPROXY_REDIS_TLS=true` when the endpoint requires TLS.

In fail-closed mode, `/ready` actively pings Redis with a bounded two-second check and returns `503` while coordination is unhealthy, so an orchestrator can remove the instance from service during the protected degraded state. `make compose-smoke` injects a Redis outage and verifies this `503` behavior. Set `MCPROXY_REDIS_FAIL_CLOSED=false` only when availability during Redis outages is the deliberate choice; monitor `mcproxy_distributed_fallbacks_total` and the corresponding alert.

When a secret manager mounts files into the container, use `MCPROXY_ADMIN_TOKEN_FILE`, optional `MCPROXY_ADMIN_ACTOR_SECRET_FILE`, `MCPROXY_POSTGRES_DSN_FILE`, `MCPROXY_REDIS_PASSWORD_FILE`, or `MCPROXY_GATE_VELOCITY_SECRET_FILE`. The corresponding direct environment variable wins if both are present. BungeeGuard does not use a global secret; its per-server token is managed in the database and must be copied to that backend's `allowed-tokens` configuration.

Before production, add TLS termination, firewall the Minecraft backends so only mcproxy can reach them, and configure Prometheus to scrape `/metrics` from an internal network.

For a Linux host deployment, [firewall/mcproxy.nft.example](firewall/mcproxy.nft.example) provides an optional per-source SYN-rate example for TCP 25565, and [firewall/sysctl-mcproxy.conf.example](firewall/sysctl-mcproxy.conf.example) documents SYN cookies and capacity-dependent backlog/conntrack settings. Review and syntax-check these before loading; they are not substitutes for provider L4 DDoS protection.

Prometheus examples are provided in [observability/prometheus.yml](observability/prometheus.yml) and [observability/alerts.yml](observability/alerts.yml). Follow [observability/README.md](observability/README.md) to validate them with the target Prometheus version, mount them into the Prometheus service, and verify the `mcproxy` target is `UP` before relying on the alerts.

After building images, run the runtime hardening smoke before publishing them. It verifies the panel's non-root runtime user, the mcproxy privilege-dropping init entrypoint, and both healthchecks:

```sh
make image-hardening-smoke
```

Then scan both locally built images with an installed image scanner. The helper supports Trivy and Grype, fails on unfixed HIGH/CRITICAL findings, and maps `IMAGE_SCAN_SEVERITY` to each tool's native flags:

```sh
make image-scan
IMAGE_SCANNER=trivy IMAGE_SCAN_SEVERITY=CRITICAL make image-scan
IMAGE_SCANNER=grype IMAGE_SCAN_SEVERITY=HIGH,CRITICAL make image-scan
```

Kubernetes templates for mcproxy and the separate admin panel are in [kubernetes](kubernetes). They expect external PostgreSQL/Redis services and pre-created Secrets; replace image tags, hostnames, TLS origins, resource limits, and ingress settings for the target cluster.

Create a PostgreSQL backup with `./deploy/backup.sh [backup-directory]`. Store the resulting custom-format dump outside the host and periodically test restoration on a separate PostgreSQL instance. The checked-in restore helper requires an explicit confirmation token:

```sh
RESTORE_CONFIRM=mcproxy-restore ./deploy/restore.sh \
  backups/mcproxy-postgres-<timestamp>.dump
```

Do not run the restore helper against production without a maintenance window and a fresh pre-restore backup. Set `POSTGRES_USER`, `POSTGRES_DB`, and `COMPOSE_FILE` when the target differs from the Compose defaults.

Both backup helpers create a sibling `.sha256` checksum when `sha256sum` or `shasum` is available. The restore helpers verify that sidecar before touching the target; keep the dump and checksum together when transferring backups.

The separate admin panel stores users, sessions, invitations, and MFA secrets in its SQLite PVC. Its `/health` endpoint is liveness-only and `/ready` verifies the session configuration, session tables, membership schema, and SQLite membership store; use those endpoints for reverse-proxy/Kubernetes probes. Back it up independently while the panel is running with `./deploy/panel-backup.sh /path/to/admin-panel.db ./backups` (the host needs `sqlite3`). Before restoring, stop the panel, take a fresh backup, and use the explicit confirmation token:

```sh
PANEL_RESTORE_CONFIRM=mcproxy-panel-restore ./deploy/panel-restore.sh \
  backups/admin-panel-<timestamp>.db /path/to/admin-panel.db
```

Never restore the panel database while the SvelteKit process is running; the restore helper validates SQLite integrity before and after replacement.
The panel restore helper also enforces mode `0600` on the restored database because it contains password hashes, sessions, invitations, and MFA secrets.

The PostgreSQL restore helper uses `pg_restore --exit-on-error` so a partially applied restore is never reported as successful. Keep the pre-restore backup until application readiness and a fixture/query check have passed.

The isolated recovery rehearsal can be run with `make postgres-restore-smoke`. It starts a disposable PostgreSQL Compose project, applies the current migration, inserts a fixture, creates a custom-format dump, restores it into a second database, and verifies the fixture. It removes the temporary containers and volumes on exit. `make panel-backup-smoke` performs the equivalent integrity-checked backup/restore fixture test for the admin panel SQLite database.
