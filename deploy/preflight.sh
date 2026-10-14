#!/bin/sh
set -eu

# Validate production-sensitive settings without printing secret values.
# Supports both direct environment variables and the *_FILE convention.

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
		if [ ! -r "$file" ]; then
			echo "$file_name points to an unreadable file" >&2
			return 1
		fi
		tr -d '\r\n' < "$file"
	fi
}

require_secret() {
	name=$1
	min_length=$2
	value=$(secret_value "$name")
	if [ -z "$value" ]; then
		echo "$name or ${name}_FILE is required" >&2
		return 1
	fi
	case "$value" in
		change-me*|replace-me*|ci-validation-only|ci-redis-validation-only|e2e-token|smoke-token)
			echo "$name contains a known non-production placeholder" >&2
			return 1
			;;
	esac
	if [ "${#value}" -lt "$min_length" ]; then
		echo "$name must contain at least $min_length characters" >&2
		return 1
	fi
}

validate_bool_env() {
	name=$1
	value=$(printenv "$name" 2>/dev/null || true)
	case "$value" in
		''|true|TRUE|1|yes|YES|on|ON|false|FALSE|0|no|NO|off|OFF)
			;;
		*)
			echo "$name must be a boolean value" >&2
			exit 1
			;;
	esac
}

for bool_name in MCPROXY_REQUIRE_POSTGRES_TLS MCPROXY_REQUIRE_REDIS_TLS MCPROXY_REDIS_TLS MCPROXY_REDIS_FAIL_CLOSED MCPROXY_GATE_PROXY_PROTOCOL MCPROXY_GATE_PROXY_PROTOCOL_BACKEND; do
	validate_bool_env "$bool_name"
done

require_postgres_tls=$(printf '%s' "${MCPROXY_REQUIRE_POSTGRES_TLS:-false}" | tr '[:upper:]' '[:lower:]')
require_redis_tls=$(printf '%s' "${MCPROXY_REQUIRE_REDIS_TLS:-false}" | tr '[:upper:]' '[:lower:]')
redis_fail_closed=$(printf '%s' "${MCPROXY_REDIS_FAIL_CLOSED:-false}" | tr '[:upper:]' '[:lower:]')
proxy_protocol=$(printf '%s' "${MCPROXY_GATE_PROXY_PROTOCOL:-false}" | tr '[:upper:]' '[:lower:]')

case "$proxy_protocol" in
	true|1|yes|on)
		if [ -z "${MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES:-}" ]; then
			echo "MCPROXY_GATE_PROXY_PROTOCOL requires MCPROXY_GATE_PROXY_PROTOCOL_TRUSTED_PROXIES" >&2
			exit 1
		fi
		;;
esac

if [ -n "${MCPROXY_GATE_BUNGEGUARD_SECRET:-}" ] || [ -n "${MCPROXY_GATE_BUNGEGUARD_SECRET_FILE:-}" ]; then
	echo "MCPROXY_GATE_BUNGEGUARD_SECRET is no longer supported; configure bungeeguard_allowed_token per server" >&2
	exit 1
fi

case "$(printf '%s' "${MCPROXY_GATE_FORWARDING_MODE:-legacy}" | tr '[:upper:]' '[:lower:]')" in
	none|legacy|velocity)
		if [ "$(printf '%s' "${MCPROXY_GATE_FORWARDING_MODE:-legacy}" | tr '[:upper:]' '[:lower:]')" = velocity ]; then
			require_secret MCPROXY_GATE_VELOCITY_SECRET 16
		fi
		;;
	bungeeguard)
		echo "MCPROXY_GATE_FORWARDING_MODE=bungeeguard is invalid; configure forwarding_mode=bungeeguard per server" >&2
		exit 1
		;;
	*)
		echo "unsupported MCPROXY_GATE_FORWARDING_MODE" >&2
		exit 1
		;;
esac

require_secret MCPROXY_ADMIN_TOKEN 32

if [ -n "$(secret_value MCPROXY_ADMIN_ACTOR_SECRET)" ]; then
	require_secret MCPROXY_ADMIN_ACTOR_SECRET 32
fi

db_driver=$(printf '%s' "${MCPROXY_DB_DRIVER:-sqlite}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
case "$db_driver" in
	sqlite)
		;;
	postgres)
		postgres_dsn=$(secret_value MCPROXY_POSTGRES_DSN)
		if [ -z "$postgres_dsn" ]; then
			echo "MCPROXY_POSTGRES_DSN or MCPROXY_POSTGRES_DSN_FILE is required for PostgreSQL" >&2
			exit 1
		fi
		case "$require_postgres_tls" in
			true|1|yes|on)
				dsn_lower=$(printf '%s' "$postgres_dsn" | tr '[:upper:]' '[:lower:]')
				case "$dsn_lower" in
					*\?sslmode=require\&*|*\&sslmode=require\&*|*\?sslmode=require|*\&sslmode=require|*\?sslmode=verify-ca\&*|*\&sslmode=verify-ca\&*|*\?sslmode=verify-ca|*\&sslmode=verify-ca|*\?sslmode=verify-full\&*|*\&sslmode=verify-full\&*|*\?sslmode=verify-full|*\&sslmode=verify-full)
						;;
					*)
						echo "MCPROXY_REQUIRE_POSTGRES_TLS requires sslmode=require, verify-ca, or verify-full" >&2
						exit 1
						;;
				 esac
				;;
			*)
				;;
		esac
		;;
	*)
		echo "unsupported MCPROXY_DB_DRIVER: ${MCPROXY_DB_DRIVER}" >&2
		exit 1
		;;
esac

if [ -n "${MCPROXY_REDIS_ADDR:-}" ]; then
	require_secret MCPROXY_REDIS_PASSWORD 16
	case "$require_redis_tls" in
		true|1|yes|on)
			redis_tls=$(printf '%s' "${MCPROXY_REDIS_TLS:-false}" | tr '[:upper:]' '[:lower:]')
			case "$redis_tls" in
				true|1|yes|on)
					;;
				*)
					echo "MCPROXY_REQUIRE_REDIS_TLS requires MCPROXY_REDIS_TLS=true" >&2
					exit 1
					;;
			esac
			;;
		*)
			;;
	esac
	case "$redis_fail_closed" in
		true|1|yes|on)
			;;
		*)
			echo "MCPROXY_REDIS_FAIL_CLOSED must be true when Redis is enabled for production" >&2
			exit 1
			;;
	esac
fi

echo "mcproxy deployment preflight OK"
