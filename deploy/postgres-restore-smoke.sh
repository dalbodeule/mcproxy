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

# Rehearse an additive upgrade from a legacy servers table. This mirrors the
# SQLite legacy migration test and proves PostgreSQL deployments do not need a
# destructive reset when the per-server forwarding/token columns are missing.
"${compose[@]}" exec -T postgres psql -U mcproxy -d mcproxy -v ON_ERROR_STOP=1 \
	-c 'ALTER TABLE servers DROP COLUMN forwarding_mode, DROP COLUMN ping_motd, DROP COLUMN bungeeguard_allowed_token;' >/dev/null
set +e
"${compose[@]}" run --rm --no-deps mcproxy migrate & first_migration=$!
"${compose[@]}" run --rm --no-deps mcproxy migrate & second_migration=$!
wait "${first_migration}"; first_status=$?
wait "${second_migration}"; second_status=$?
set -e
if [[ "${first_status}" -ne 0 || "${second_status}" -ne 0 ]]; then
	echo "concurrent PostgreSQL migrations failed: ${first_status}, ${second_status}" >&2
	exit 1
fi
legacy_columns="$(${compose[@]} exec -T postgres psql -U mcproxy -d mcproxy -At -v ON_ERROR_STOP=1 \
	-c "SELECT count(*) FROM information_schema.columns WHERE table_name = 'servers' AND column_name IN ('forwarding_mode', 'ping_motd', 'bungeeguard_allowed_token');" | tr -d '\r[:space:]')"
if [[ "${legacy_columns}" != "3" ]]; then
	echo "PostgreSQL additive migration did not restore all server columns: ${legacy_columns}" >&2
	exit 1
fi

# Add a record so the restore check proves data, not only an empty schema, was
# recovered. The smoke project is disposable and is removed by cleanup.
"${compose[@]}" exec -T postgres psql -U mcproxy -d mcproxy -v ON_ERROR_STOP=1 \
	-c "INSERT INTO servers (name, upstream, ping_motd, bungeeguard_allowed_token, priority, enabled, created_at, updated_at) VALUES ('restore-smoke', '127.0.0.1:25565', 'restore smoke', '0123456789abcdef0123456789abcdef', 100, true, NOW(), NOW());" >/dev/null

"${compose[@]}" exec -T postgres pg_dump -U mcproxy -d mcproxy --format=custom --no-owner --no-privileges >"${backup_file}"
checksum_file="${backup_file}.sha256"
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum "${backup_file}" >"${checksum_file}"
elif command -v shasum >/dev/null 2>&1; then
	shasum -a 256 "${backup_file}" >"${checksum_file}"
else
	echo "sha256sum or shasum is required for the PostgreSQL restore smoke" >&2
	exit 2
fi
expected="$(awk '{print $1}' "${checksum_file}")"
if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "${backup_file}" | awk '{print $1}')"
else
	actual="$(shasum -a 256 "${backup_file}" | awk '{print $1}')"
fi
[[ -n "${expected}" && "${actual}" == "${expected}" ]] || { echo "backup checksum verification failed" >&2; exit 1; }
"${compose[@]}" exec -T postgres psql -U mcproxy -d postgres -v ON_ERROR_STOP=1 \
	-c 'CREATE DATABASE mcproxy_restore;' >/dev/null
"${compose[@]}" exec -T postgres pg_restore -U mcproxy -d mcproxy_restore \
	--clean --if-exists --exit-on-error --no-owner --no-privileges <"${backup_file}"

restored_count="$("${compose[@]}" exec -T postgres psql -U mcproxy -d mcproxy_restore -At \
	-c "SELECT count(*) FROM servers WHERE name = 'restore-smoke';" | tr -d '\r[:space:]')"
if [[ "${restored_count}" != "1" ]]; then
	echo "restore verification failed: expected 1 restored server, got ${restored_count}" >&2
	exit 1
fi

echo "PostgreSQL backup/restore smoke test OK"
