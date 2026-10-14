#!/usr/bin/env bash
set -Eeuo pipefail

# The panel database contains password hashes, sessions, invitations, and
# TOTP secrets. Keep both newly-created and pre-existing targets private.
umask 077

backup_file="${1:-}"
target_db="${2:-./admin-panel/data/admin-panel.db}"
if [[ -z "$backup_file" || ! -f "$backup_file" ]]; then
  echo "usage: PANEL_RESTORE_CONFIRM=mcproxy-panel-restore $0 <backup.db> [target.db]" >&2
  exit 2
fi
if [[ "${PANEL_RESTORE_CONFIRM:-}" != "mcproxy-panel-restore" ]]; then
  echo "refusing restore: stop the panel and set PANEL_RESTORE_CONFIRM=mcproxy-panel-restore" >&2
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
if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "sqlite3 is required for panel database restore" >&2
  exit 2
fi

if [[ "$(sqlite3 "$backup_file" 'PRAGMA integrity_check;')" != "ok" ]]; then
  echo "refusing restore: backup integrity check failed" >&2
  exit 2
fi
mkdir -p "$(dirname "$target_db")"
sqlite3 "$target_db" ".restore '$backup_file'"
if [[ "$(sqlite3 "$target_db" 'PRAGMA integrity_check;')" != "ok" ]]; then
  echo "restore completed but target integrity check failed" >&2
  exit 1
fi
chmod 600 "$target_db"
printf 'Restored %s to %s\n' "$backup_file" "$target_db"
