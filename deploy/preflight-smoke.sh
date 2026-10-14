#!/bin/sh
set -eu

script=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/preflight.sh
base_env='MCPROXY_DB_DRIVER=postgres MCPROXY_POSTGRES_DSN=postgres://mcproxy:smoke-dsn@db:5432/mcproxy MCPROXY_ADMIN_TOKEN=smoke-admin-token-012345678901234567890'

env $base_env "$script" >/dev/null

if env $base_env MCPROXY_REQUIRE_POSTGRES_TLS=true "$script" >/dev/null 2>&1; then
	echo 'preflight accepted a PostgreSQL DSN without explicit TLS' >&2
	exit 1
fi
env $base_env MCPROXY_REQUIRE_POSTGRES_TLS=true MCPROXY_POSTGRES_DSN='postgres://mcproxy:smoke-dsn@db:5432/mcproxy?sslmode=verify-full' "$script" >/dev/null
env $base_env MCPROXY_REQUIRE_POSTGRES_TLS=ON MCPROXY_POSTGRES_DSN='postgres://mcproxy:smoke-dsn@db:5432/mcproxy?sslmode=verify-full' "$script" >/dev/null

if env $base_env MCPROXY_REQUIRE_POSTGRES_TLS=true MCPROXY_POSTGRES_DSN='postgres://mcproxy:smoke-dsn@db:5432/mcproxy?sslmode=disable' "$script" >/dev/null 2>&1; then
	echo 'preflight accepted PostgreSQL sslmode=disable with TLS required' >&2
	exit 1
fi
if env $base_env MCPROXY_REQUIRE_POSTGRES_TLS=true MCPROXY_POSTGRES_DSN='postgres://mcproxy:smoke-dsn@db:5432/mcproxy?notsslmode=require' "$script" >/dev/null 2>&1; then
	echo 'preflight accepted a non-sslmode query parameter as TLS' >&2
	exit 1
fi

if env $base_env MCPROXY_REDIS_ADDR=redis:6379 MCPROXY_REDIS_PASSWORD=smoke-redis-password MCPROXY_REDIS_FAIL_CLOSED=true MCPROXY_REQUIRE_REDIS_TLS=true "$script" >/dev/null 2>&1; then
	echo 'preflight accepted Redis without explicit TLS' >&2
	exit 1
fi
env $base_env MCPROXY_REDIS_ADDR=redis:6379 MCPROXY_REDIS_PASSWORD=smoke-redis-password MCPROXY_REDIS_FAIL_CLOSED=true MCPROXY_REQUIRE_REDIS_TLS=true MCPROXY_REDIS_TLS=true "$script" >/dev/null
env $base_env MCPROXY_REDIS_ADDR=redis:6379 MCPROXY_REDIS_PASSWORD=smoke-redis-password MCPROXY_REDIS_FAIL_CLOSED=ON MCPROXY_REQUIRE_REDIS_TLS=1 MCPROXY_REDIS_TLS=yes "$script" >/dev/null

if env $base_env MCPROXY_GATE_FORWARDING_MODE=velocity "$script" >/dev/null 2>&1; then
	echo 'preflight accepted Velocity mode without its secret' >&2
	exit 1
fi
env $base_env MCPROXY_GATE_FORWARDING_MODE=velocity MCPROXY_GATE_VELOCITY_SECRET=smoke-velocity-secret "$script" >/dev/null

if env $base_env MCPROXY_ADMIN_ACTOR_SECRET=short "$script" >/dev/null 2>&1; then
	echo 'preflight accepted a short admin actor secret' >&2
	exit 1
fi
env $base_env MCPROXY_ADMIN_ACTOR_SECRET=smoke-admin-actor-secret-012345678901234567890 "$script" >/dev/null

env $base_env MCPROXY_DB_DRIVER=' POSTGRES ' "$script" >/dev/null

if env $base_env MCPROXY_GATE_PROXY_PROTOCOL=true "$script" >/dev/null 2>&1; then
	echo 'preflight accepted PROXY protocol without trusted proxies' >&2
	exit 1
fi
env $base_env MCPROXY_GATE_PROXY_PROTOCOL=ON MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES=10.0.0.0/8 "$script" >/dev/null

if env $base_env MCPROXY_REQUIRE_POSTGRES_TLS=maybe "$script" >/dev/null 2>&1; then
	echo 'preflight accepted malformed PostgreSQL TLS boolean' >&2
	exit 1
fi

if env $base_env MCPROXY_GATE_BUNGEGUARD_SECRET=legacy-global-secret "$script" >/dev/null 2>&1; then
	echo 'preflight accepted the removed global BungeeGuard secret' >&2
	exit 1
fi

output=$(mktemp)
trap 'rm -f "$output"' EXIT INT TERM
if env $base_env MCPROXY_GATE_FORWARDING_MODE=bungeeguard "$script" >"$output" 2>&1; then
	echo 'preflight accepted the invalid global BungeeGuard mode' >&2
	exit 1
fi
grep -F 'configure forwarding_mode=bungeeguard per server' "$output" >/dev/null
echo 'Deployment preflight smoke test OK'
