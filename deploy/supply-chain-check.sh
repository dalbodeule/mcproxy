#!/bin/sh
set -eu

root=${1:-.}
workflow="$root/.github/workflows/ci.yml"

[ -r "$workflow" ] || { echo "workflow is not readable: $workflow" >&2; exit 1; }

# Every GitHub Action must use an immutable 40-hex commit rather than a tag.
action_refs=$(sed -nE 's/^[[:space:]-]*uses:[[:space:]]*[^@[:space:]]+@([^[:space:]#]+).*/\1/p' "$workflow")
[ -n "$action_refs" ] || { echo 'workflow contains no pinned actions' >&2; exit 1; }
while IFS= read -r ref; do
	printf '%s\n' "$ref" | grep -Eq '^[0-9a-fA-F]{40}$' || {
		echo "GitHub Action is not a 40-hex commit: $ref" >&2
		exit 1
	}
done <<EOF
$action_refs
EOF

check_digest_images() {
	file=$1
	while IFS= read -r image; do
		case "$image" in
			*@sha256:*) digest=${image##*@sha256:} ;;
			*) echo "image is not digest-pinned: $image" >&2; return 1 ;;
		esac
		printf '%s\n' "$digest" | grep -Eq '^[0-9a-fA-F]{64}$' || {
			echo "image digest is not a 64-hex SHA-256: $image" >&2
			return 1
		}
	done <<EOF
$(sed -nE 's/^[[:space:]]*image:[[:space:]]*([^[:space:]#]+).*/\1/p' "$file")
EOF
}

check_digest_images "$root/deploy/docker-compose.yml"

for file in "$root/Dockerfile" "$root/admin-panel/Dockerfile"; do
	froms=$(sed -nE 's/^[[:space:]]*FROM[[:space:]]+([^[:space:]]+).*/\1/p' "$file")
	while IFS= read -r image; do
		case "$image" in
			*@sha256:*) digest=${image##*@sha256:} ;;
			*) echo "base image is not digest-pinned in $file: $image" >&2; exit 1 ;;
		esac
		printf '%s\n' "$digest" | grep -Eq '^[0-9a-fA-F]{64}$' || {
			echo "base image digest is not a 64-hex SHA-256: $image" >&2
			exit 1
		}
	done <<EOF
$froms
EOF
done

echo 'Supply-chain pinning check OK'
