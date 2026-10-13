#!/usr/bin/env bash
set -Eeuo pipefail

# Rehearses the documented PostgreSQL recovery path in an isolated Compose
# project. It never touches a developer's default Compose volume.
project="mcproxy-postgres-restore-smoke-$$"
backup_dir="$(mktemp -d)"
backup_file="${backup_dir}/mcproxy.dump"
compose=(docker compose -p "${project}" -f deploy/docker-compose.yml)

export POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-restore-smoke-postgres}"
export REDIS_PASSWORD="${REDIS_PASSWORD:-restore-smoke-redis}"

cleanup() {
	"${compose[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
	rm -rf "${backup_dir}"
}
trap cleanup EXIT INT TERM

"${compose[@]}" up -d --build postgres
for attempt in $(seq 1 30); do
	if "${compose[@]}" exec -T postgres pg_isready -U mcproxy -d mcproxy >/dev/null 2>&1; then
		break
	fi
	if [[ "${attempt}" == 30 ]]; then
		echo "PostgreSQL did not become ready" >&2
		exit 1
	fi
	sleep 2
done

# Apply the same migration command used by production deployment.
"${compose[@]}" run --rm --no-deps mcproxy migrate

# Add a record so the restore check proves data, not only an empty schema, was
# recovered. The smoke project is disposable and is removed by cleanup.
"${compose[@]}" exec -T postgres psql -U mcproxy -d mcproxy -v ON_ERROR_STOP=1 \
	-c "INSERT INTO servers (name, upstream, ping_motd, bungeeguard_allowed_token, priority, enabled, created_at, updated_at) VALUES ('restore-smoke', '127.0.0.1:25565', 'restore smoke', '0123456789abcdef0123456789abcdef', 100, true, NOW(), NOW());" >/dev/null

"${compose[@]}" exec -T postgres pg_dump -U mcproxy -d mcproxy --format=custom --no-owner --no-privileges >"${backup_file}"
"${compose[@]}" exec -T postgres psql -U mcproxy -d postgres -v ON_ERROR_STOP=1 \
	-c 'CREATE DATABASE mcproxy_restore;' >/dev/null
"${compose[@]}" exec -T postgres pg_restore -U mcproxy -d mcproxy_restore \
	--clean --if-exists --no-owner --no-privileges <"${backup_file}"

restored_count="$("${compose[@]}" exec -T postgres psql -U mcproxy -d mcproxy_restore -At \
	-c "SELECT count(*) FROM servers WHERE name = 'restore-smoke';" | tr -d '\r[:space:]')"
if [[ "${restored_count}" != "1" ]]; then
	echo "restore verification failed: expected 1 restored server, got ${restored_count}" >&2
	exit 1
fi

echo "PostgreSQL backup/restore smoke test OK"
