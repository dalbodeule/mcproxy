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

Status

- Implemented foundation:
  - HTTP admin API
  - entgo-backed persistence
  - Geo policy evaluation
  - Minekube Gate runtime bootstrap and pre-login denial path
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
- Base runtime image: [`gcr.io/distroless/base-debian13:nonroot`](Dockerfile:12)
- Builder uses Go 1.27 (`go.mod` and `Dockerfile`).

Backend forwarding

- `MCPROXY_GATE_FORWARDING_MODE` accepts `none`, `legacy`, `velocity`, or `bungeeguard` (default: `legacy`).
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
  - `MCPROXY_REDIS_CHANNEL`
  - `MCPROXY_GEOIP_PATH`
  - `MCPROXY_ADMIN_TOKEN`
  - `MCPROXY_ADMIN_ALLOWED_ORIGINS` (optional exact origins for the separate admin panel)

Operations

- `GET /health` is a liveness check; `GET /ready` verifies database readiness.
- `GET /metrics` exposes a small Prometheus-compatible set of process and policy metrics.
- The separate SvelteKit 3+ / TailwindCSS admin panel should keep the token server-side and proxy API requests through its server routes. If browser requests go directly to mcproxy, configure `MCPROXY_ADMIN_ALLOWED_ORIGINS` with exact HTTPS origins.
- Use PostgreSQL and Redis for multi-instance production; SQLite is for single-instance deployments.

Quick Links

- Full spec and API (Korean): [README.ko.md](README.ko.md)
- Progress tracking: [progress.md](progress.md)
- License: [LICENSE.md](LICENSE.md)

License

AGPL-3.0. See [LICENSE.md](LICENSE.md).
