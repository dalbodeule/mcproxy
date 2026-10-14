#!/bin/sh
set -eu

if [ "$#" -eq 0 ]; then
	echo "usage: $0 RENDERED_MANIFEST [RENDERED_MANIFEST ...]" >&2
	exit 2
fi

combined=$(mktemp)
trap 'rm -f "$combined"' EXIT INT TERM

for file in "$@"; do
	[ -r "$file" ] || { echo "manifest is not readable: $file" >&2; exit 1; }
	case "$file" in
		*.yaml|*.yml) ;;
		*) echo "manifest must be YAML: $file" >&2; exit 1 ;;
	esac
	cat "$file" >> "$combined"
done

if grep -nE 'your-registry/|replace-with-immutable-tag|redis\.example\.internal|admin\.example\.com|panel\.example\.invalid|mcproxy-secret-store|mcproxy/prod/|198\.51\.100\.|203\.0\.113\.|192\.0\.2\.' "$combined" >/dev/null 2>&1; then
	echo 'rendered manifest still contains a known deployment placeholder' >&2
	exit 1
fi
if grep -nE '^[[:space:]]*cidr:[[:space:]]*(0\.0\.0\.0/0|::/0)[[:space:]]*$' "$combined" >/dev/null 2>&1; then
	echo 'rendered NetworkPolicy must not allow unrestricted CIDR egress' >&2
	exit 1
fi

if grep -Eq '^[[:space:]]*kind:[[:space:]]*ExternalSecret[[:space:]]*$' "$combined"; then
	for key in admin-token postgres-dsn redis-password username password session-secret; do
		count=$(grep -Ec "^[[:space:]]*-[[:space:]]*secretKey:[[:space:]]*$key[[:space:]]*$" "$combined" || true)
		case "$key" in
			admin-token) [ "$count" -ge 2 ] || { echo 'ExternalSecret mappings must include admin-token for mcproxy and panel' >&2; exit 1; } ;;
			postgres-dsn|redis-password) [ "$count" -ge 1 ] || { echo "ExternalSecret mapping is missing: $key" >&2; exit 1; } ;;
			*) [ "$count" -ge 1 ] || { echo "ExternalSecret mapping is missing: $key" >&2; exit 1; } ;;
		esac
	done
	actor_count=$(grep -Ec '^[[:space:]]*-[[:space:]]*secretKey:[[:space:]]*admin-actor-secret[[:space:]]*$' "$combined" || true)
	if [ "$actor_count" -ne 0 ] && [ "$actor_count" -ne 2 ]; then
		echo 'admin-actor-secret must be mapped exactly once in both mcproxy and panel ExternalSecrets' >&2
		exit 1
	fi
	if [ "$actor_count" -eq 2 ]; then
		actor_keys=$(awk '
			/^[[:space:]]*-[[:space:]]*secretKey:[[:space:]]*admin-actor-secret[[:space:]]*$/ { in_actor = 1; next }
			in_actor && /^[[:space:]]*key:[[:space:]]*/ { print $2; in_actor = 0 }
		' "$combined")
		unique_actor_keys=$(printf '%s\n' "$actor_keys" | awk 'NF && !seen[$0]++ { count++ } END { print count + 0 }')
		if [ "$unique_actor_keys" -ne 1 ]; then
			echo 'both admin-actor-secret mappings must use the same remoteRef.key' >&2
			exit 1
		fi
	fi
fi

image_count=0
while IFS= read -r line; do
	image_count=$((image_count + 1))
	image=${line#*: }
	case "$image" in
		*@sha256:*) digest=${image##*@sha256:} ;;
		*) echo "deployment image is not digest-pinned: $image" >&2; exit 1 ;;
	esac
	if [ "${#digest}" -ne 64 ] || ! printf '%s' "$digest" | grep -Eq '^[0-9a-fA-F]{64}$'; then
		echo "deployment image has an invalid sha256 digest: $image" >&2
		exit 1
	fi
done <<EOF
$(grep -E '^[[:space:]]*image:' "$combined" || true)
EOF

if [ "$image_count" -lt 2 ]; then
	echo 'rendered production manifests must include images for mcproxy and the admin panel' >&2
	exit 1
fi
grep -Eq '^[[:space:]]*image:[[:space:]]*(.*/)?mcproxy([:@]|$)' "$combined" || {
	echo 'rendered production manifests must include a mcproxy image' >&2
	exit 1
}
grep -Eq '^[[:space:]]*image:[[:space:]]*(.*/)?mcproxy-admin-panel([:@]|$)' "$combined" || {
	echo 'rendered production manifests must include an admin panel image' >&2
	exit 1
}

grep -Eq '^[[:space:]]*automountServiceAccountToken:[[:space:]]*false[[:space:]]*$' "$combined" || {
	echo 'manifest must disable automatic ServiceAccount token mounting' >&2
	exit 1
}
grep -Eq '^[[:space:]]*readOnlyRootFilesystem:[[:space:]]*true[[:space:]]*$' "$combined" || {
	echo 'manifest must use read-only root filesystems' >&2
	exit 1
}
grep -Eq '^[[:space:]]*allowPrivilegeEscalation:[[:space:]]*false[[:space:]]*$' "$combined" || {
	echo 'manifest must disable privilege escalation' >&2
	exit 1
}
nonroot_workloads=$(grep -Ec '^[[:space:]]*runAsNonRoot:[[:space:]]*true[[:space:]]*$' "$combined" || true)
if [ "$nonroot_workloads" -lt 3 ]; then
	echo 'manifest must explicitly require non-root policy for mcproxy, HA mcproxy, and admin panel' >&2
	exit 1
fi
mcproxy_uids=$(grep -Ec '^[[:space:]]*runAsUser:[[:space:]]*65532[[:space:]]*$' "$combined" || true)
if [ "$mcproxy_uids" -lt 2 ]; then
	echo 'manifest must run both mcproxy workloads as UID 65532' >&2
	exit 1
fi
mcproxy_gids=$(grep -Ec '^[[:space:]]*runAsGroup:[[:space:]]*65532[[:space:]]*$' "$combined" || true)
if [ "$mcproxy_gids" -lt 2 ]; then
	echo 'manifest must run both mcproxy workloads as GID 65532' >&2
	exit 1
fi
grep -Eq '^[[:space:]]*fsGroup:[[:space:]]*65532[[:space:]]*$' "$combined" || {
	echo 'mcproxy manifest must provision /data with the nonroot fsGroup 65532' >&2
	exit 1
}
grep -Eq 'MCPROXY_REDIS_FAIL_CLOSED' "$combined" || {
	echo 'manifest must configure Redis fail-closed policy explicitly' >&2
	exit 1
}
if grep -Eq 'MCPROXY_REDIS_FAIL_CLOSED:[[:space:]]*false' "$combined"; then
	echo 'production manifest must not configure Redis fail-open' >&2
	exit 1
fi
require_true_env() {
	name=$1
	if ! awk -v wanted="$name" '
		$0 ~ "^[[:space:]]*-[[:space:]]*name:[[:space:]]*" wanted "[[:space:]]*$" { seen = 1; next }
		seen && $0 ~ "^[[:space:]]*value:[[:space:]]*" { if ($0 ~ /(^|[[:space:]])("?true"?)([[:space:]]|$)/) ok = 1; seen = 0 }
		END { exit ok ? 0 : 1 }
	' "$combined"; then
		echo "manifest must set $name=true explicitly" >&2
		exit 1
	fi
}

require_true_env MCPROXY_REQUIRE_POSTGRES_TLS
require_true_env MCPROXY_REQUIRE_REDIS_TLS
require_true_env MCPROXY_REDIS_TLS

grep -Eq '^[[:space:]]*externalTrafficPolicy:[[:space:]]*Local[[:space:]]*$' "$combined" || {
	echo 'Minecraft LoadBalancer must preserve client source addresses with externalTrafficPolicy=Local' >&2
	exit 1
}
grep -Eq '^[[:space:]]*startupProbe:[[:space:]]*$' "$combined" || {
	echo 'workloads must define a startupProbe to separate slow initialization from liveness failures' >&2
	exit 1
}

echo 'Kubernetes production manifest check OK'
