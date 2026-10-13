#!/usr/bin/env bash
set -euo pipefail

backup_dir="${1:-./backups}"
umask 077
mkdir -p "$backup_dir"

timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
output="$backup_dir/mcproxy-postgres-$timestamp.dump"
partial="$output.partial.$$"
trap 'rm -f "$partial"' EXIT

docker compose -f deploy/docker-compose.yml exec -T postgres \
	pg_dump -U mcproxy -d mcproxy --format=custom --no-owner --no-privileges >"$partial"
mv "$partial" "$output"

printf 'Created %s\n' "$output"
