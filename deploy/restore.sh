#!/usr/bin/env bash
set -Eeuo pipefail

backup_file="${1:-}"
compose_file="${COMPOSE_FILE:-deploy/docker-compose.yml}"
pg_user="${POSTGRES_USER:-mcproxy}"
pg_database="${POSTGRES_DB:-mcproxy}"

if [[ -z "${backup_file}" || ! -f "${backup_file}" ]]; then
  echo "usage: RESTORE_CONFIRM=mcproxy-restore $0 <custom-format-backup.dump>" >&2
  exit 2
fi
if [[ "${RESTORE_CONFIRM:-}" != "mcproxy-restore" ]]; then
  echo "refusing restore: set RESTORE_CONFIRM=mcproxy-restore after taking a fresh backup" >&2
  exit 2
fi

echo "Restoring ${backup_file} into ${pg_database} via ${compose_file}."
echo "This replaces objects in the target database."
docker compose -f "${compose_file}" exec -T postgres \
  pg_restore -U "${pg_user}" -d "${pg_database}" \
  --clean --if-exists --no-owner --no-privileges < "${backup_file}"
