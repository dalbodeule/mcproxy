#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT INT TERM

sed \
	-e 's#your-registry/mcproxy:replace-with-immutable-tag#registry.example/mcproxy@sha256:0000000000000000000000000000000000000000000000000000000000000000#' \
	-e 's#redis.example.internal:6379#redis.prod.internal:6379#' \
	"$root/deploy/kubernetes/mcproxy.yaml" > "$tmpdir/mcproxy.yaml"
sed \
	-e 's#your-registry/mcproxy:replace-with-immutable-tag#registry.example/mcproxy@sha256:2222222222222222222222222222222222222222222222222222222222222222#' \
	-e 's#redis.example.internal:6379#redis.prod.internal:6379#' \
	"$root/deploy/kubernetes/ha/mcproxy.yaml" > "$tmpdir/mcproxy-ha.yaml"
sed \
	-e 's#your-registry/mcproxy-admin-panel:replace-with-immutable-tag#registry.example/mcproxy-admin-panel@sha256:1111111111111111111111111111111111111111111111111111111111111111#' \
	-e 's#https://admin.example.com#https://panel.prod.example#' \
	"$root/deploy/kubernetes/admin-panel.yaml" > "$tmpdir/admin-panel.yaml"
sed \
	-e 's#name: mcproxy-secret-store#name: production-secret-store#' \
	-e 's#mcproxy/prod/#production/mcproxy/#g' \
	"$root/deploy/kubernetes/external-secrets.example.yaml" > "$tmpdir/external-secrets.yaml"

"$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy.yaml" "$tmpdir/mcproxy-ha.yaml" "$tmpdir/admin-panel.yaml" "$tmpdir/external-secrets.yaml"

if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy.yaml" "$tmpdir/mcproxy-ha.yaml" "$tmpdir/admin-panel.yaml" "$root/deploy/kubernetes/external-secrets.example.yaml" >/dev/null 2>&1; then
	echo 'production check accepted an ExternalSecret manifest with provider placeholders' >&2
	exit 1
fi

sed '/secretKey: session-secret/d' "$tmpdir/external-secrets.yaml" > "$tmpdir/external-secrets-no-session-secret.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy.yaml" "$tmpdir/mcproxy-ha.yaml" "$tmpdir/admin-panel.yaml" "$tmpdir/external-secrets-no-session-secret.yaml" >/dev/null 2>&1; then
	echo 'production check accepted an ExternalSecret manifest without the panel session secret' >&2
	exit 1
fi

cp "$tmpdir/external-secrets.yaml" "$tmpdir/external-secrets-one-actor-secret.yaml"
printf '\n      - secretKey: admin-actor-secret\n' >> "$tmpdir/external-secrets-one-actor-secret.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy.yaml" "$tmpdir/mcproxy-ha.yaml" "$tmpdir/admin-panel.yaml" "$tmpdir/external-secrets-one-actor-secret.yaml" >/dev/null 2>&1; then
	echo 'production check accepted an ExternalSecret manifest with only one actor secret mapping' >&2
	exit 1
fi

cp "$tmpdir/external-secrets.yaml" "$tmpdir/external-secrets-mismatched-actor-secret.yaml"
cp "$tmpdir/external-secrets.yaml" "$tmpdir/external-secrets-matching-actor-secret.yaml"
cat >> "$tmpdir/external-secrets-matching-actor-secret.yaml" <<'EOF'
      - secretKey: admin-actor-secret
        remoteRef:
          key: production/mcproxy/actor-secret
      - secretKey: admin-actor-secret
        remoteRef:
          key: production/mcproxy/actor-secret
EOF
"$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy.yaml" "$tmpdir/mcproxy-ha.yaml" "$tmpdir/admin-panel.yaml" "$tmpdir/external-secrets-matching-actor-secret.yaml"

cat >> "$tmpdir/external-secrets-mismatched-actor-secret.yaml" <<'EOF'
      - secretKey: admin-actor-secret
        remoteRef:
          key: production/mcproxy/actor-secret-a
      - secretKey: admin-actor-secret
        remoteRef:
          key: production/mcproxy/actor-secret-b
EOF
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy.yaml" "$tmpdir/mcproxy-ha.yaml" "$tmpdir/admin-panel.yaml" "$tmpdir/external-secrets-mismatched-actor-secret.yaml" >/dev/null 2>&1; then
	echo 'production check accepted ExternalSecrets with mismatched actor secret keys' >&2
	exit 1
fi

printf '\n    # broad egress regression fixture\n    cidr: 0.0.0.0/0\n' >> "$tmpdir/admin-panel.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy.yaml" "$tmpdir/mcproxy-ha.yaml" "$tmpdir/admin-panel.yaml" >/dev/null 2>&1; then
	echo 'production check accepted unrestricted CIDR egress' >&2
	exit 1
fi

sed '/MCPROXY_REQUIRE_POSTGRES_TLS/{N;d;}' "$tmpdir/mcproxy.yaml" > "$tmpdir/mcproxy-no-postgres-tls.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy-no-postgres-tls.yaml" "$tmpdir/admin-panel.yaml" >/dev/null 2>&1; then
	echo 'production check accepted a manifest without PostgreSQL TLS enforcement' >&2
	exit 1
fi

sed '/MCPROXY_REQUIRE_REDIS_TLS/{N;d;}' "$tmpdir/mcproxy.yaml" > "$tmpdir/mcproxy-no-redis-tls.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy-no-redis-tls.yaml" "$tmpdir/admin-panel.yaml" >/dev/null 2>&1; then
	echo 'production check accepted a manifest without Redis TLS enforcement' >&2
	exit 1
fi

sed '/externalTrafficPolicy: Local/d' "$tmpdir/mcproxy.yaml" > "$tmpdir/mcproxy-no-source-preservation.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy-no-source-preservation.yaml" "$tmpdir/admin-panel.yaml" >/dev/null 2>&1; then
	echo 'production check accepted a Minecraft LoadBalancer without source preservation' >&2
	exit 1
fi

sed '/fsGroup: 65532/d' "$tmpdir/mcproxy.yaml" > "$tmpdir/mcproxy-no-nonroot-fsgroup.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy-no-nonroot-fsgroup.yaml" "$tmpdir/admin-panel.yaml" >/dev/null 2>&1; then
	echo 'production check accepted a mcproxy manifest without the nonroot fsGroup' >&2
	exit 1
fi

sed '/runAsNonRoot: true/d' "$tmpdir/mcproxy.yaml" > "$tmpdir/mcproxy-no-nonroot-policy.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy-no-nonroot-policy.yaml" "$tmpdir/admin-panel.yaml" >/dev/null 2>&1; then
	echo 'production check accepted a workload without an explicit non-root policy' >&2
	exit 1
fi

sed '/runAsUser: 65532/d' "$tmpdir/mcproxy.yaml" > "$tmpdir/mcproxy-no-nonroot-uid.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy-no-nonroot-uid.yaml" "$tmpdir/admin-panel.yaml" >/dev/null 2>&1; then
	echo 'production check accepted mcproxy without the required UID' >&2
	exit 1
fi

sed '/runAsGroup: 65532/d' "$tmpdir/mcproxy.yaml" > "$tmpdir/mcproxy-no-nonroot-gid.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy-no-nonroot-gid.yaml" "$tmpdir/admin-panel.yaml" >/dev/null 2>&1; then
	echo 'production check accepted mcproxy without the required GID' >&2
	exit 1
fi

sed '/^[[:space:]]*image:/d' "$tmpdir/admin-panel.yaml" > "$tmpdir/admin-panel-no-image.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy.yaml" "$tmpdir/admin-panel-no-image.yaml" >/dev/null 2>&1; then
	echo 'production check accepted a workload without a container image' >&2
	exit 1
fi

sed 's#registry.example/mcproxy@#registry.example/other-mcproxy@#' "$tmpdir/mcproxy.yaml" > "$tmpdir/mcproxy-wrong-image.yaml"
if "$root/deploy/kubernetes/production-check.sh" "$tmpdir/mcproxy-wrong-image.yaml" "$tmpdir/admin-panel.yaml" >/dev/null 2>&1; then
	echo 'production check accepted a manifest without the mcproxy image' >&2
	exit 1
fi

echo 'Kubernetes production manifest check negative cases OK'
