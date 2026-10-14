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

if [[ -f "${backup_file}.sha256" ]]; then
  expected="$(awk '{print $1}' "${backup_file}.sha256")"
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "${backup_file}" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "${backup_file}" | awk '{print $1}')"
  else
    echo "cannot verify ${backup_file}.sha256: neither sha256sum nor shasum is installed" >&2
    exit 2
  fi
  [[ -n "$expected" && "$actual" == "$expected" ]] || { echo "refusing restore: backup checksum mismatch" >&2; exit 2; }
fi

echo "Restoring ${backup_file} into ${pg_database} via ${compose_file}."
echo "This replaces objects in the target database."
docker compose -f "${compose_file}" exec -T postgres \
  pg_restore -U "${pg_user}" -d "${pg_database}" \
  --clean --if-exists --exit-on-error --no-owner --no-privileges < "${backup_file}"
