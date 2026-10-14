#!/usr/bin/env bash
set -euo pipefail

source_db="${1:-./admin-panel/data/admin-panel.db}"
backup_dir="${2:-./backups}"
if [[ ! -f "$source_db" ]]; then
  echo "panel SQLite database not found: $source_db" >&2
  exit 2
fi
if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "sqlite3 is required for an online panel database backup" >&2
  exit 2
fi

if [[ "$(sqlite3 "$source_db" 'PRAGMA integrity_check;')" != "ok" ]]; then
  echo "refusing backup: source integrity check failed" >&2
  exit 1
fi

umask 077
mkdir -p "$backup_dir"
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
output="$backup_dir/admin-panel-$timestamp.db"
partial="$output.partial.$$"
checksum_partial="$output.sha256.partial.$$"
trap 'rm -f "$partial" "$checksum_partial"' EXIT

sqlite3 "$source_db" ".backup '$partial'"
sqlite3 "$partial" 'PRAGMA integrity_check;' | grep -qx 'ok'
mv "$partial" "$output"
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$output" >"$checksum_partial"
elif command -v shasum >/dev/null 2>&1; then
  shasum -a 256 "$output" >"$checksum_partial"
else
  echo "warning: neither sha256sum nor shasum is installed; checksum sidecar was not created" >&2
fi
if [[ -s "$checksum_partial" ]]; then
  mv "$checksum_partial" "$output.sha256"
fi
printf 'Created %s\n' "$output"
