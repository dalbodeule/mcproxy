#!/usr/bin/env bash
set -euo pipefail

backup_dir="${1:-./backups}"
umask 077
mkdir -p "$backup_dir"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
output="$backup_dir/mcproxy-postgres-$timestamp.dump"
partial="$output.partial.$$"
checksum_partial="$output.sha256.partial.$$"
trap 'rm -f "$partial" "$checksum_partial"' EXIT

docker compose -f deploy/docker-compose.yml exec -T postgres \
	pg_dump -U mcproxy -d mcproxy --format=custom --no-owner --no-privileges >"$partial"
if [[ ! -s "$partial" ]]; then
	echo "PostgreSQL backup is empty" >&2
	exit 1
fi
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
