#!/bin/sh
set -eu

script=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/panel-preflight.sh
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT INT TERM

base_env='MCPROXY_API_URL=http://mcproxy:8080 ORIGIN=https://panel.example.invalid MCPROXY_ADMIN_TOKEN=smoke-panel-admin-token-012345678901234567 ADMIN_PANEL_SESSION_SECRET=smoke-panel-session-secret-012345678901234567'

env $base_env ADMIN_PANEL_DB_PATH="$tmpdir/admin-panel.db" PANEL_PREFLIGHT_REQUIRE_HTTPS=true "$script" >/dev/null
env $base_env ADMIN_PANEL_DB_PATH="$tmpdir/admin-panel.db" PANEL_PREFLIGHT_REQUIRE_HTTPS=TRUE "$script" >/dev/null

if env $base_env ADMIN_PANEL_DB_PATH="$tmpdir/admin-panel.db" ORIGIN=http://panel.example.invalid PANEL_PREFLIGHT_REQUIRE_HTTPS=true "$script" >/dev/null 2>&1; then
	echo 'panel preflight accepted insecure ORIGIN with HTTPS required' >&2
	exit 1
fi
if env $base_env ADMIN_PANEL_DB_PATH="$tmpdir/admin-panel.db" PANEL_PREFLIGHT_REQUIRE_HTTPS=maybe "$script" >/dev/null 2>&1; then
	echo 'panel preflight accepted malformed HTTPS boolean' >&2
	exit 1
fi
if env $base_env ADMIN_PANEL_DB_PATH="$tmpdir/admin-panel.db" MCPROXY_ADMIN_ACTOR_SECRET=short "$script" >/dev/null 2>&1; then
	echo 'panel preflight accepted a short admin actor secret' >&2
	exit 1
fi
env $base_env ADMIN_PANEL_DB_PATH="$tmpdir/admin-panel.db" MCPROXY_ADMIN_ACTOR_SECRET=smoke-panel-actor-secret-012345678901234567890 "$script" >/dev/null

mkdir "$tmpdir/readonly"
chmod 555 "$tmpdir/readonly"
if env $base_env ADMIN_PANEL_DB_PATH="$tmpdir/readonly/admin-panel.db" "$script" >/dev/null 2>&1; then
	echo 'panel preflight accepted a read-only database directory' >&2
	exit 1
fi

echo 'Admin panel preflight smoke test OK'
