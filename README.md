mcproxy — Minecraft Security Proxy built on Minekube Gate

Language: 한국어 문서는 [README.ko.md](README.ko.md)

Overview

mcproxy is a security-focused proxy for Minecraft (Java Edition) built on top of Minekube Gate (Go). It adds a management plane via HTTP API and data-plane protections such as IP/nickname DoS throttling and GeoIP-based country blocking, with per-server and global thresholds, backed by entgo ORM with SQLite (default) or PostgreSQL.

See [SECURITY.md](SECURITY.md) for trust boundaries, production controls, known limits, and vulnerability reporting guidance.

Key Features

- Built on Minekube Gate (Go)
- Management plane: HTTP API for runtime policy control (block/allow, thresholds, GeoIP rules)
- DoS-suspect throttling by IP and nickname (global and per-server)
- GeoLite2 (City) database–based country allow/deny
- Per-server configuration of thresholds and policies
- entgo ORM with SQLite or PostgreSQL
- In-memory policy/rule snapshot cache for faster evaluation
- 1-second-slot sliding-window counters with optional Redis sorted-set coordination
- Async audit event pipeline
- Distroless multi-stage container image via [Dockerfile](Dockerfile)
- Separate SvelteKit 3+ / TailwindCSS operations panel in [admin-panel](admin-panel)
- Admin panel locale switching for Korean, English, and Japanese, with server-side translation loading

Status

- Current implementation is ready for controlled staging deployment. It is not a
  hosted service; TLS, SecretStore, firewall, GeoIP, egress CIDR, and Prometheus
  threshold values remain deployment-specific.
- Implemented foundation:
  - HTTP admin API
  - entgo-backed persistence
  - Geo policy evaluation
  - Minekube Gate runtime bootstrap, pre-login/login denial, server-switch policy checks, and connection lifecycle audit events
  - Redis-based distributed counter/cache invalidation hooks
  - PostgreSQL/Redis/Gate environment examples in [inc.env](inc.env)

Runtime Components

- HTTP API server entrypoint: [cmd/mcproxy/main.go](cmd/mcproxy/main.go)
- Gate runtime bootstrap: [internal/gate/runtime.go](internal/gate/runtime.go)
- Policy evaluation store: [internal/store/store.go](internal/store/store.go)
- Snapshot cache: [internal/store/cache.go](internal/store/cache.go)
- Distributed coordination: [internal/store/distributed.go](internal/store/distributed.go)

Container

- Build image: `docker build -t mcproxy .`
- Base runtime image: `gcr.io/distroless/base-debian13`; the image declares `USER nonroot:nonroot` (UID/GID 65532) before exec. The init wrapper retains compatibility for explicit root overrides.
- Builder uses Go 1.27 (`go.mod` and `Dockerfile`).
- The image defaults HTTP to `0.0.0.0:8080` and SQLite to `/data/mcproxy.db`; override these with environment variables when needed.

Validation commands

- `make check` runs Go tests and vet.
- `make release-check` runs the core Go/race/panel/security/deployment/preflight checks in one repeatable command. Go test and race targets fail after five minutes by default (`GO_TEST_TIMEOUT` can override it), preventing a stuck Gate fixture from occupying CI indefinitely. `make security-audit` can also run the Go `govulncheck` and admin-panel high-severity `npm audit` gates independently.
- `make ops-rehearsal` runs the release gate plus the disposable Gate protocol 777, PostgreSQL/Redis fail-closed, PostgreSQL restore, and admin-panel runtime rehearsals as one pre-deployment check.
- `make supply-chain-check` verifies that GitHub Actions use immutable commit SHAs and that Docker/Compose images use SHA-256 digests.
- `make config-doc-check` verifies that supported runtime/preflight environment variable names appear in both README locales and `inc.env`; `make config-doc-check-smoke` also verifies that a missing entry is rejected.
- `make panel-check` runs the SvelteKit type check and production build.
- `make panel-rehearsal` runs the containerized panel login, membership, migration, security, and backup/restore rehearsals.
- `make validate-deploy` validates Compose and deployment YAML templates.
- `make docker-images` builds both local deployment images.
- Local Make builds derive the mcproxy version from the Git tag/description and commit when available; override with `VERSION`, `COMMIT`, and `BUILD_TIME` for reproducible release builds.
- `make gate-e2e` builds the image and runs a real Minecraft 26.3 status smoke test against Gate. Login packet forwarding is covered by the Gate integration tests, which attach a backend fixture; a standalone `mcproxy-statuscheck -login` can be used against a Gate instance with an enabled backend.
- `make compose-smoke` starts the PostgreSQL/Redis Compose stack and probes the mcproxy readiness healthcheck.
- `make image-hardening-smoke` verifies the built container images retain non-root panel execution, mcproxy privilege dropping, and healthchecks before publishing.
- `make panel-smoke` runs the separate admin-panel image and checks its login endpoint/security headers.
- `make migrate` builds the binary and applies the current additive Ent schema without starting Gate.
- `make migration-smoke` verifies the same migration command inside the distroless image.

Database migrations

- Normal startup applies the current Ent schema automatically without dropping existing data.
- For a controlled deployment, back up first and run `mcproxy migrate` before restarting the service. With Compose: `docker compose -f deploy/docker-compose.yml run --rm mcproxy migrate`.
- The BungeeGuard token and per-server `ping_motd` columns are added to existing SQLite/PostgreSQL databases by the same migration path; legacy empty token rows receive a token when servers are listed.

Backend forwarding

- `MCPROXY_GATE_FORWARDING_MODE` accepts `none`, `legacy`, or `velocity` (default: `legacy`). Set each server's `forwarding_mode` through the server API/admin panel; BungeeGuard is only valid per backend and cannot be set globally. This lets one Gate instance route legacy, no-forwarding, and BungeeGuard backends independently. Per-server `velocity` requires the global mode to remain `velocity` because Velocity forwarding uses Gate's login-plugin flow.
- `MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP` optionally limits simultaneous modern-client login setup per source IP; `0` disables it. With Redis enabled, reservations are shared across Gate replicas; without Redis, the limit is per process. It protects the application layer after TCP acceptance and does not replace an L4 firewall/conntrack limit.
- In per-server BungeeGuard mode, mcproxy puts that backend's `bungeeguard_allowed_token` into its handshake. For the official BungeeGuard plugin, add the exact value from `/api/v1/servers/` as one item under `allowed-tokens:` in `plugins/BungeeGuard/config.yml`, remove placeholder/old tokens, and run `bungeeguard reload`. Rotate it from the admin panel or `POST /api/v1/servers/{id}/bungeeguard-token`; update and reload the backend plugin immediately after rotation. `allowed-token` (singular) is not the official key, and a global `MCPROXY_GATE_BUNGEGUARD_SECRET` is rejected by deployment preflight because BungeeGuard is strictly per-server.
- Each server can also set `ping_motd` through the server API or admin panel; hostname-routed status pings use that MOTD and unmatched hosts use the fallback MOTD.

For Minecraft 26.3 clients, rebuild and redeploy the image so the bundled Gate `v0.74.41` is actually running; this version includes protocol 777 support and the login `hello` encoding fix. If the client then reports `Unable to authenticate`, align the forwarding mode and secret with the backend: Paper 1.13.2+ should use Velocity modern forwarding, while older Paper/Spigot setups use legacy or BungeeGuard.
- For Paper/Spigot with legacy forwarding, set backend `online-mode=false` and `settings.bungeecord=true`; firewall the backend so clients cannot bypass mcproxy.
- For Velocity forwarding, configure the same secret on Gate and the backend's Velocity forwarding support.
- In Docker, `localhost` refers to the mcproxy container. Use the other container's service name or a host address reachable from the container for the backend upstream.

Environment

- Example environment file: [inc.env](inc.env)
- Key variables:
  - `MCPROXY_HTTP_ADDR`
  - `MCPROXY_GATE_BIND`
  - `MCPROXY_GATE_ONLINE_MODE`
  - `MCPROXY_GATE_PROXY_PROTOCOL` and `MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES` (only behind a trusted L4 proxy; preserves client IP for Gate quotas)
  - `MCPROXY_GATE_PROXY_PROTOCOL_BACKEND` (only when upstream Minecraft servers expect PROXY protocol)
  - `MCPROXY_GATE_FORWARDING_MODE`
  - `MCPROXY_GATE_VELOCITY_SECRET` (when using Velocity forwarding)
  - `MCPROXY_GATE_MAX_CONCURRENT_LOGINS_PER_IP` (optional application-level per-IP login reservation limit)
  - `MCPROXY_DB_DRIVER`
  - `MCPROXY_SQLITE_PATH` (SQLite database path; defaults to `data/mcproxy.db`)
  - `MCPROXY_POSTGRES_DSN`
  - `MCPROXY_REQUIRE_POSTGRES_TLS` (deployment preflight guard for explicit PostgreSQL TLS)
  - `MCPROXY_REDIS_ADDR`
  - `MCPROXY_REDIS_PASSWORD` and `MCPROXY_REDIS_TLS` (when Redis authentication/TLS is enabled)
  - `MCPROXY_REQUIRE_REDIS_TLS` (deployment preflight guard for explicit Redis TLS)
  - `MCPROXY_REDIS_CHANNEL`
  - `MCPROXY_REDIS_FAIL_CLOSED` (Compose production default: `true`; deny evaluations when Redis counters fail; set `false` explicitly for local fallback)
  - `MCPROXY_GEOIP_PATH` (optional GeoLite2 City database for country rules)
  - `MCPROXY_GEOIP_ASN_PATH` (optional GeoLite2 ASN database for `asn-allow`/`asn-deny` rules)
  - `MCPROXY_ADMIN_TOKEN`
  - `MCPROXY_ADMIN_ACTOR_SECRET` (optional 32+ character HMAC secret shared with the panel)
  - `MCPROXY_ADMIN_THROTTLE` (optional API backlog limit; defaults to `32`)
  - `MCPROXY_ADMIN_ALLOWED_ORIGINS` (optional exact origins for the separate admin panel)
  - `MCPROXY_TRUST_PROXY_HEADERS` and `MCPROXY_TRUSTED_PROXY_CIDRS` (optional; only enable together behind a trusted proxy that rewrites client-IP headers; without matching CIDRs headers are ignored)
  - `MCPROXY_PING_CACHE_TTL_SEC`, `MCPROXY_PING_FALLBACK_MOTD`, `MCPROXY_PING_OFFLINE_MOTD`, `MCPROXY_PING_FALLBACK_FAVICON` (optional server-list ping fallback settings)
  - `MCPROXY_LOKI_HOST` and `MCPROXY_LOG_IDENTIFY` (optional Loki endpoint and log identity)

Sensitive settings also accept the standard `*_FILE` form for Docker/Kubernetes Secret mounts, such as `MCPROXY_ADMIN_TOKEN_FILE=/run/secrets/admin-token`. A direct environment variable takes precedence. This applies to the admin token, optional panel actor-signing secret, PostgreSQL DSN, Redis password, and the global Velocity forwarding secret. BungeeGuard has no global secret; its generated token is stored and selected per backend.

Operations

- `GET /health` is a liveness check; `GET /ready` verifies database and Gate data-plane readiness in the main process and actively checks Redis with a bounded timeout when fail-closed coordination is enabled.
- `GET /metrics` exposes Prometheus-compatible process, policy, Gate readiness, connected-player, registered-backend, evaluated-attempt, allowed-attempt, and process-lifetime decision-block metrics.
- `mcproxy_build_info` in `/metrics` exposes the running release version, commit, build time, and Go runtime version without adding high-cardinality labels. Release builds can inject them with `VERSION`, `COMMIT`, and `BUILD_TIME` Make variables or the matching Docker build arguments.
- `GET /metrics` also exposes Redis coordination errors/fallbacks and whether fail-closed mode is enabled; review these metrics in multi-instance deployments.
- `mcproxy_audit_write_errors_total` counts failed audit persistence operations. Treat any increase as a critical database/storage incident because security and administrative events may be missing.
- `mcproxy_distributed_healthy` distinguishes a currently degraded Redis connection from historical coordination errors after recovery.
- SIGTERM triggers HTTP shutdown and waits for the Gate runtime to finish draining within the shutdown timeout.
- Metrics include `mcproxy_blocks_by_reason_total{reason="..."}` for runtime block reasons.
- Metrics include both `mcproxy_servers` and `mcproxy_enabled_servers` so disabled backends do not mask an unavailable data plane.
- When the main process owns Gate, `mcproxy_gate_ready` reports whether the Gate data plane is running.
- Prometheus scrape and baseline alert examples are in [deploy/observability](deploy/observability).
- `GET /api/v1/audits` returns recent audit events for the operations panel.
- `PUT /api/v1/servers/{id}` accepts `forwarding_mode` for per-backend forwarding selection; an empty value uses the global compatibility fallback.
- `POST /api/v1/servers/{id}/bungeeguard-token` generates and immediately syncs a new 32-character per-backend BungeeGuard token.
- Administrative server, policy, Geo, and rule mutations are recorded with actor `admin-api`; Gate connection, disconnect, backend rejection, and block events are also recorded.
- To preserve panel member identity in audit logs, set the same random `MCPROXY_ADMIN_ACTOR_SECRET` on mcproxy and the separate panel (or its `MCPROXY_ADMIN_ACTOR_SECRET_FILE`). The panel then signs the actor header; unsigned or invalid actor headers fall back to `admin-api`.
- Gate remains listening when no backend is enabled, so enabling a server from the admin panel is picked up by the runtime sync loop without a restart.
- Admin API authentication failures are limited to 20 per source IP per minute; excess attempts receive `429` with `Retry-After: 60`.
- The separate SvelteKit 3+ / TailwindCSS admin panel should keep the token server-side and proxy API requests through its server routes. If browser requests go directly to mcproxy, configure `MCPROXY_ADMIN_ALLOWED_ORIGINS` with exact HTTPS origins.
- Optional `asn-allow` and `asn-deny` rules use the MaxMind GeoLite2-ASN database configured by `MCPROXY_GEOIP_ASN_PATH`; use the same rules API and targets such as `AS12345`.
- The panel also supports its own signed, httpOnly session using `ADMIN_PANEL_PASSWORD` and a 32+ character `ADMIN_PANEL_SESSION_SECRET`; still deploy it behind TLS and an identity-aware reverse proxy or VPN.
- The panel's Korean/English/Japanese locale and future invite/RBAC membership plan are documented in [docs/admin-panel-membership.md](docs/admin-panel-membership.md).
- The current implementation and future targets are tracked in [docs/roadmap.md](docs/roadmap.md). The next priority is staging operations: backend forwarding compatibility, token rotation/rollback, real provider secrets, backup RTO/RPO, and release artifact verification.
- Use PostgreSQL and Redis for multi-instance production; SQLite is for single-instance deployments.
- Compose uses `POSTGRES_SSLMODE=disable` only for its local internal-network default; use `require` or `verify-full` with a provisioned CA for production networks.
- The container includes a `/app/mcproxy healthcheck` command used by Docker to check `/ready` without requiring curl or a shell in the distroless image.
- The root `.dockerignore` excludes credentials, local databases, Git metadata, and the separate panel's dependency/build directories from the mcproxy image context.

Quick Links

- Full spec and API (Korean): [README.ko.md](README.ko.md)
- HTTP API reference: [docs/api-reference.md](docs/api-reference.md)
- Progress tracking: [progress.md](progress.md)
- Roadmap and future goals: [docs/roadmap.md](docs/roadmap.md)
- License: [LICENSE.md](LICENSE.md)

License

AGPL-3.0. See [LICENSE.md](LICENSE.md).
