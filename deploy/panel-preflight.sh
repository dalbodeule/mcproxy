#!/bin/sh
set -eu

# Validate the separate SvelteKit admin panel without printing credentials.
# The *_FILE convention matches the panel's server-only secret loader.

secret_value() {
	name=$1
	file_name=${name}_FILE
	value=$(printenv "$name" 2>/dev/null || true)
	file=$(printenv "$file_name" 2>/dev/null || true)
	if [ -n "$value" ]; then
		printf '%s' "$value"
		return 0
	fi
	if [ -n "$file" ]; then
		[ -r "$file" ] || { echo "$file_name points to an unreadable file" >&2; return 1; }
		tr -d '\r\n' < "$file"
	fi
}

require_secret() {
	name=$1
	min_length=$2
	value=$(secret_value "$name")
	[ -n "$value" ] || { echo "$name or ${name}_FILE is required" >&2; return 1; }
	case "$value" in
		change-me*|replace-me*|smoke-token|ci-*)
			echo "$name contains a known non-production placeholder" >&2
			return 1
			;;
	esac
	[ "${#value}" -ge "$min_length" ] || { echo "$name must contain at least $min_length characters" >&2; return 1; }
}

case "${PANEL_PREFLIGHT_REQUIRE_HTTPS:-}" in
	''|true|TRUE|1|yes|YES|on|ON|false|FALSE|0|no|NO|off|OFF)
		;;
	*)
		echo 'PANEL_PREFLIGHT_REQUIRE_HTTPS must be a boolean value' >&2
		exit 1
		;;
esac

require_https=$(printf '%s' "${PANEL_PREFLIGHT_REQUIRE_HTTPS:-false}" | tr '[:upper:]' '[:lower:]')
case "$require_https" in
	1|yes|on)
		require_https=true
		;;
	0|no|off|'')
		require_https=false
		;;
esac

require_secret MCPROXY_ADMIN_TOKEN 32
require_secret ADMIN_PANEL_SESSION_SECRET 32

if [ -n "$(secret_value MCPROXY_ADMIN_ACTOR_SECRET)" ]; then
	require_secret MCPROXY_ADMIN_ACTOR_SECRET 32
fi

api_url=${MCPROXY_API_URL:-}
case "$api_url" in
	http://*|https://*)
		;;
	*)
		echo 'MCPROXY_API_URL must be an http:// or https:// URL' >&2
		exit 1
		;;
esac

origin=${ORIGIN:-}
[ -n "$origin" ] || { echo 'ORIGIN is required' >&2; exit 1; }
case "$origin" in
	http://*|https://*)
		;;
	*)
		echo 'ORIGIN must be an http:// or https:// URL' >&2
		exit 1
		;;
esac
if [ "$require_https" = true ] && [ "${origin#https://}" = "$origin" ]; then
	echo 'ORIGIN must use https:// when PANEL_PREFLIGHT_REQUIRE_HTTPS=true' >&2
	exit 1
fi

db_path=${ADMIN_PANEL_DB_PATH:-/data/admin-panel.db}
[ "$db_path" != ':memory:' ] || { echo 'ADMIN_PANEL_DB_PATH=:memory: is not allowed for deployment' >&2; exit 1; }
db_dir=$(dirname "$db_path")
[ -d "$db_dir" ] || { echo "ADMIN_PANEL_DB_PATH directory does not exist: $db_dir" >&2; exit 1; }
if [ -e "$db_path" ] && [ ! -w "$db_path" ]; then
	echo "ADMIN_PANEL_DB_PATH is not writable: $db_path" >&2
	exit 1
fi
probe="$db_dir/.mcproxy-panel-preflight.$$"
trap 'rm -f "$probe"' EXIT INT TERM
if ! (umask 077; : > "$probe") 2>/dev/null; then
	echo "ADMIN_PANEL_DB_PATH directory is not writable: $db_dir" >&2
	exit 1
fi

echo 'admin panel deployment preflight OK'
