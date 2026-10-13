mcproxy — Minecraft Security Proxy built on Minekube Gate

Language: 한국어 문서는 [README.ko.md](README.ko.md)

Overview

mcproxy is a security-focused proxy for Minecraft (Java Edition) built on top of Minekube Gate (Go). It adds a management plane via HTTP API and data-plane protections such as IP/nickname DoS throttling and GeoIP-based country blocking, with per-server and global thresholds, backed by entgo ORM with SQLite (default) or PostgreSQL.

Key Features

- Built on Minekube Gate (Go)
- Management plane: HTTP API for runtime policy control (block/allow, thresholds, GeoIP rules)
- DoS-suspect throttling by IP and nickname (global and per-server)
- GeoLite2 (City) database–based country allow/deny
- Per-server configuration of thresholds and policies
- entgo ORM with SQLite or PostgreSQL
- In-memory policy/rule snapshot cache for faster evaluation
- Sliding-window-style in-memory counter path with optional Redis coordination
- Async audit event pipeline
- Distroless multi-stage container image via [Dockerfile](Dockerfile)
- Separate SvelteKit 3+ / TailwindCSS operations panel in [admin-panel](admin-panel)

Status

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
- Base runtime image: `gcr.io/distroless/base-debian13`; the init wrapper drops the application to UID/GID 65532 before exec.
- Builder uses Go 1.27 (`go.mod` and `Dockerfile`).
- The image defaults HTTP to `0.0.0.0:8080` and SQLite to `/data/mcproxy.db`; override these with environment variables when needed.

Validation commands

- `make check` runs Go tests and vet.
- `make panel-check` runs the SvelteKit type check and production build.
- `make validate-deploy` validates Compose and deployment YAML templates.
- `make docker-images` builds both local deployment images.
- `make gate-e2e` builds the image and runs a real Minecraft Status handshake smoke test against Gate.
- `make compose-smoke` starts the PostgreSQL/Redis Compose stack and probes the mcproxy readiness healthcheck.
- `make panel-smoke` runs the separate admin-panel image and checks its login endpoint/security headers.
- `make migrate` builds the binary and applies the current additive Ent schema without starting Gate.
- `make migration-smoke` verifies the same migration command inside the distroless image.

Database migrations

- Normal startup applies the current Ent schema automatically without dropping existing data.
- For a controlled deployment, back up first and run `mcproxy migrate` before restarting the service. With Compose: `docker compose -f deploy/docker-compose.yml run --rm mcproxy migrate`.
- The BungeeGuard token and per-server `ping_motd` columns are added to existing SQLite/PostgreSQL databases by the same migration path; legacy empty token rows receive a token when servers are listed.

Backend forwarding

- `MCPROXY_GATE_FORWARDING_MODE` accepts `none`, `legacy`, `velocity`, or the deprecated `bungeeguard` compatibility value (default: `legacy`). Prefer setting each server's `forwarding_mode` through the server API/admin panel; BungeeGuard is never inherited from the global value and must be explicitly selected per backend. This lets one Gate instance route legacy, no-forwarding, and BungeeGuard backends independently. Per-server `velocity` requires the global mode to remain `velocity` because Velocity forwarding uses Gate's login-plugin flow.
- In per-server BungeeGuard mode, mcproxy puts that backend's `bungeeguard_allowed_token` into its handshake. For the official BungeeGuard plugin, add the exact value from `/api/v1/servers/` as one item under `allowed-tokens:` in `plugins/BungeeGuard/config.yml`, remove placeholder/old tokens, and run `bungeeguard reload`. Rotate it from the admin panel or `POST /api/v1/servers/{id}/bungeeguard-token`; update and reload the backend plugin immediately after rotation. `allowed-token` (singular) is not the official key, and `MCPROXY_GATE_BUNGEGUARD_SECRET` is not required for this per-server mode.
- Each server can also set `ping_motd` through the server API or admin panel; hostname-routed status pings use that MOTD and unmatched hosts use the fallback MOTD.

For Minecraft 26.3 clients, rebuild and redeploy the image so the bundled Gate `v0.74.40` is actually running; this version includes protocol 777 support and the login `hello` encoding fix. If the client then reports `Unable to authenticate`, align the forwarding mode and secret with the backend: Paper 1.13.2+ should use Velocity modern forwarding, while older Paper/Spigot setups use legacy or BungeeGuard.
- For Paper/Spigot with legacy forwarding, set backend `online-mode=false` and `settings.bungeecord=true`; firewall the backend so clients cannot bypass mcproxy.
- For Velocity forwarding, configure the same secret on Gate and the backend's Velocity forwarding support.
- In Docker, `localhost` refers to the mcproxy container. Use the other container's service name or a host address reachable from the container for the backend upstream.

Environment

- Example environment file: [inc.env](inc.env)
- Key variables:
  - `MCPROXY_HTTP_ADDR`
  - `MCPROXY_GATE_BIND`
  - `MCPROXY_GATE_ONLINE_MODE`
  - `MCPROXY_GATE_FORWARDING_MODE`
  - `MCPROXY_GATE_VELOCITY_SECRET` (when using Velocity forwarding)
  - `MCPROXY_DB_DRIVER`
  - `MCPROXY_POSTGRES_DSN`
  - `MCPROXY_REDIS_ADDR`
  - `MCPROXY_REDIS_PASSWORD` and `MCPROXY_REDIS_TLS` (when Redis authentication/TLS is enabled)
  - `MCPROXY_REDIS_CHANNEL`
  - `MCPROXY_GEOIP_PATH`
  - `MCPROXY_ADMIN_TOKEN`
  - `MCPROXY_ADMIN_ALLOWED_ORIGINS` (optional exact origins for the separate admin panel)
  - `MCPROXY_TRUST_PROXY_HEADERS` (optional; enable only behind a trusted proxy that rewrites client-IP headers)

Sensitive settings also accept the standard `*_FILE` form for Docker/Kubernetes Secret mounts, such as `MCPROXY_ADMIN_TOKEN_FILE=/run/secrets/admin-token`. A direct environment variable takes precedence. This applies to the admin token, PostgreSQL DSN, Redis password, and Gate forwarding secrets.

Operations

- `GET /health` is a liveness check; `GET /ready` verifies database and Gate data-plane readiness in the main process.
- `GET /metrics` exposes Prometheus-compatible process, policy, Gate readiness, connected-player, and registered-backend metrics.
- SIGTERM triggers HTTP shutdown and waits for the Gate runtime to finish draining within the shutdown timeout.
- Metrics include `mcproxy_blocks_by_reason_total{reason="..."}` for runtime block reasons.
- Metrics include both `mcproxy_servers` and `mcproxy_enabled_servers` so disabled backends do not mask an unavailable data plane.
- When the main process owns Gate, `mcproxy_gate_ready` reports whether the Gate data plane is running.
- Prometheus scrape and baseline alert examples are in [deploy/observability](deploy/observability).
- `GET /api/v1/audits` returns recent audit events for the operations panel.
- `POST/PUT /api/v1/servers/{id}` accepts `forwarding_mode` for per-backend forwarding selection; an empty value uses the global compatibility fallback.
- Administrative server, policy, Geo, and rule mutations are recorded with actor `admin-api`; Gate connection, disconnect, backend rejection, and block events are also recorded.
- Gate remains listening when no backend is enabled, so enabling a server from the admin panel is picked up by the runtime sync loop without a restart.
- Admin API authentication failures are limited to 20 per source IP per minute; excess attempts receive `429` with `Retry-After: 60`.
- The separate SvelteKit 3+ / TailwindCSS admin panel should keep the token server-side and proxy API requests through its server routes. If browser requests go directly to mcproxy, configure `MCPROXY_ADMIN_ALLOWED_ORIGINS` with exact HTTPS origins.
- The panel also supports its own signed, httpOnly session using `ADMIN_PANEL_PASSWORD` and a 32+ character `ADMIN_PANEL_SESSION_SECRET`; still deploy it behind TLS and an identity-aware reverse proxy or VPN.
- Use PostgreSQL and Redis for multi-instance production; SQLite is for single-instance deployments.
- Compose uses `POSTGRES_SSLMODE=disable` only for its local internal-network default; use `require` or `verify-full` with a provisioned CA for production networks.
- The container includes a `/app/mcproxy healthcheck` command used by Docker to check `/ready` without requiring curl or a shell in the distroless image.
- The root `.dockerignore` excludes credentials, local databases, Git metadata, and the separate panel's dependency/build directories from the mcproxy image context.

Quick Links

- Full spec and API (Korean): [README.ko.md](README.ko.md)
- Progress tracking: [progress.md](progress.md)
- License: [LICENSE.md](LICENSE.md)

License

AGPL-3.0. See [LICENSE.md](LICENSE.md).
