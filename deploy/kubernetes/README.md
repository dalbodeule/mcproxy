# Kubernetes deployment templates

These manifests assume PostgreSQL and Redis are managed services or are deployed separately. Create the referenced Secrets before applying the manifests, and replace the example image names with immutable registry tags or digests.

After rendering provider-specific values and immutable image digests into deployment copies, run `./deploy/kubernetes/production-check.sh rendered-mcproxy.yaml rendered-admin-panel.yaml`. Do not run this check against the checked-in examples; those intentionally contain placeholders and must fail until rendered for the target cluster.

```sh
kubectl create namespace mcproxy --dry-run=client -o yaml | kubectl apply -f -
kubectl -n mcproxy create secret generic mcproxy-runtime \
  --from-file=admin-token=/run/secrets/mcproxy-admin-token \
  --from-file=postgres-dsn=/run/secrets/mcproxy-postgres-dsn \
  --from-file=redis-password=/run/secrets/mcproxy-redis-password \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n mcproxy create secret generic mcproxy-panel \
  --from-file=admin-token=/run/secrets/mcproxy-admin-token \
  --from-file=username=/run/secrets/panel-username \
  --from-file=password=/run/secrets/panel-password \
  --from-file=session-secret=/run/secrets/panel-session-secret \
  --dry-run=client -o yaml | kubectl apply -f -

# Validate against the target cluster's API schema before changing workloads.
kubectl apply --server-side --dry-run=server -f deploy/kubernetes/mcproxy.yaml
kubectl apply --server-side --dry-run=server -f deploy/kubernetes/admin-panel.yaml
kubectl apply --server-side --dry-run=server -f deploy/kubernetes/network-policy.yaml
kubectl apply --server-side --dry-run=server -f deploy/kubernetes/external-secrets.example.yaml # optional

kubectl apply -f deploy/kubernetes/mcproxy.yaml
kubectl apply -f deploy/kubernetes/admin-panel.yaml
kubectl apply -f deploy/kubernetes/network-policy.yaml
kubectl -n mcproxy rollout status deployment/mcproxy --timeout=5m
kubectl -n mcproxy rollout status deployment/mcproxy-admin-panel --timeout=5m
```

For a horizontally scaled Gate data plane, use the PostgreSQL/Redis-backed HA manifest instead of applying the base mcproxy Deployment directly:

```sh
kubectl apply --server-side --dry-run=server -f deploy/kubernetes/ha/mcproxy.yaml
kubectl apply -f deploy/kubernetes/ha/mcproxy.yaml
kubectl -n mcproxy rollout status deployment/mcproxy --timeout=5m
kubectl -n mcproxy get pods -l app.kubernetes.io/name=mcproxy
```

The mcproxy manifests set `externalTrafficPolicy: Local` on the Minecraft LoadBalancer so supported Kubernetes providers preserve the client source address for per-IP policy and rate limiting. All workloads use a startup probe so database migration or Gate initialization does not trigger premature liveness restarts. The cloud load balancer must health-check the Minecraft port and distribute new TCP connections; verify its source-IP behavior before enabling per-IP limits. The HA manifest runs two mcproxy replicas with a rolling update, keeps the HTTP API internal, and does not mount the single-instance `/data` PVC. This manifest does not solve SYN-flood/conntrack exhaustion, which must still be handled by the provider or an upstream L4 firewall.

Run `MCPROXY_DB_DRIVER=postgres MCPROXY_POSTGRES_DSN_FILE=/run/secrets/mcproxy-postgres-dsn MCPROXY_REQUIRE_POSTGRES_TLS=true MCPROXY_REDIS_ADDR=redis.example.internal:6379 MCPROXY_REDIS_PASSWORD_FILE=/run/secrets/mcproxy-redis-password MCPROXY_REDIS_TLS=true MCPROXY_REQUIRE_REDIS_TLS=true MCPROXY_REDIS_FAIL_CLOSED=true MCPROXY_ADMIN_TOKEN_FILE=/run/secrets/mcproxy-admin-token ./deploy/preflight.sh` in the deployment environment before applying. The command validates configuration without printing secret contents, requires an explicit PostgreSQL `sslmode=require`, `verify-ca`, or `verify-full`, and requires Redis TLS. Keep the secret files outside the repository and readable only by the deployment operator.

Run the separate panel preflight with the panel's server-only secret files before applying its Deployment:

```sh
MCPROXY_API_URL=http://mcproxy-http:8080 \
ORIGIN=https://admin.example.com \
MCPROXY_ADMIN_TOKEN_FILE=/run/secrets/mcproxy-admin-token \
ADMIN_PANEL_SESSION_SECRET_FILE=/run/secrets/panel-session-secret \
ADMIN_PANEL_DB_PATH=/data/admin-panel.db \
PANEL_PREFLIGHT_REQUIRE_HTTPS=true ./deploy/panel-preflight.sh
```

The panel Secret must provide `admin-token`, `username`, `password`, and `session-secret` keys as referenced by `admin-panel.yaml`. The preflight checks the token and session secret; the username/password remain inside the Kubernetes Secret and are consumed only by the panel bootstrap path.

The `admin-actor-secret` key is optional, but when used it must contain the same random value in both `mcproxy-runtime` and `mcproxy-panel`. It signs the panel member identity sent to mcproxy for audit attribution; keep it separate from the admin API token.

The mcproxy image declares `USER nonroot:nonroot` and `exec`s the application as UID 65532. The template also explicitly sets `runAsNonRoot`, UID/GID 65532, and mounts a `PersistentVolumeClaim` at `/data` with `fsGroup: 65532` and `OnRootMismatch`; configure a suitable StorageClass and size for the target cluster. Do not override the entrypoint or force a different user unless the image is rebuilt with an equivalent volume-permission strategy. The admin-panel image already runs as the non-root `node` user.

Both workloads disable automatic ServiceAccount token mounting and allow 30 seconds for graceful termination. The separate admin panel uses one replica because its membership/invitation store is SQLite on a `ReadWriteOnce` volume; move to a shared database adapter before scaling it horizontally.

Keep the HTTP service and `/metrics` internal. Expose only the Minecraft TCP service and the panel through a TLS-terminating ingress or load balancer. The example NetworkPolicies assume an `ingress-nginx` controller and Prometheus pods labelled `prometheus: "true"`; adjust those selectors for the target cluster. Configure managed-database backups and an external Secret Manager before production.

The checked-in mcproxy Deployments set `MCPROXY_REDIS_FAIL_CLOSED=true` because Redis is part of this production template's protection boundary, and explicitly require TLS for both PostgreSQL and Redis. The PostgreSQL DSN Secret must therefore include `sslmode=require`, `verify-ca`, or `verify-full`; change Redis fail-closed to `false` only when availability during Redis outages is the explicit priority.

For the panel, the TLS terminator must overwrite `X-Forwarded-Proto` and the deployment must set `PROTOCOL_HEADER=x-forwarded-proto` only when that proxy is trusted. Do not pass arbitrary client forwarding headers through to the panel.

## External Secret Manager

`external-secrets.example.yaml` is an optional External Secrets Operator template. It is not included in the default `kubectl apply` commands because the `ClusterSecretStore` and remote key names are provider-specific. After installing the operator, create a provider-specific store, replace the store name and `remoteRef.key` values, then apply it:

```sh
kubectl apply -f deploy/kubernetes/external-secrets.example.yaml
kubectl -n mcproxy get externalsecret mcproxy-runtime mcproxy-panel
kubectl -n mcproxy rollout status deployment/mcproxy
kubectl -n mcproxy rollout status deployment/mcproxy-admin-panel
```

The generated Secret names and keys match the two workload manifests. Provider credentials are not stored in this repository. To enable signed audit actors with External Secrets, uncomment the identical `admin-actor-secret` mapping in both data sections and point it at one high-entropy remote secret. Rotate remote values through the secret manager and let the operator refresh the Kubernetes Secrets. The workloads read these values at process startup; restart the affected deployment after the Secret refresh:

```sh
# Restart only the data plane or only the panel when possible.
NAMESPACE=mcproxy ./deploy/kubernetes/rotate-secrets.sh mcproxy
NAMESPACE=mcproxy ./deploy/kubernetes/rotate-secrets.sh panel
# Use all only when the shared admin token or a coordinated rotation changed.
NAMESPACE=mcproxy ./deploy/kubernetes/rotate-secrets.sh all
```

The helper performs a rollout status check and does not expose Secret values. Set `ROLLOUT_TIMEOUT` when the cluster needs a longer replacement window.

If a rollout fails, inspect the previous ReplicaSet and restore the last known image/configuration explicitly; do not delete the PVC. For a workload-only rollback, use `kubectl -n mcproxy rollout undo deployment/mcproxy` or `deployment/mcproxy-admin-panel`, then wait for the corresponding rollout status and `/ready` response.

The example NetworkPolicy restricts ingress only. `network-policy-egress.example.yaml` provides an optional default-deny egress starting point. Replace its documentation CIDRs and DNS labels before applying it: mcproxy needs PostgreSQL, Redis, Minecraft upstreams, and optional GeoIP access, while the panel needs the mcproxy HTTP service. Add a provider-specific restricted GeoIP CIDR if needed; do not replace it with `0.0.0.0/0:443`. A blanket egress deny without those destinations will make the deployment unavailable.

## Rollout rehearsal

After rendering immutable images, provider-specific SecretStore values, and real egress CIDRs, run the server-side validation rehearsal:

```sh
MCPROXY_MANIFEST=/path/to/rendered-mcproxy.yaml \
PANEL_MANIFEST=/path/to/rendered-admin-panel.yaml \
NETWORK_POLICY_MANIFEST=/path/to/rendered-network-policy.yaml \
EGRESS_MANIFEST=/path/to/rendered-egress-policy.yaml \
./deploy/kubernetes/rollout-rehearsal.sh
```

The default is dry-run only. It rejects documentation CIDRs and validates every manifest against the target cluster API. Apply and wait for both deployments only after the dry-run succeeds:

```sh
APPLY=true CONFIRM_APPLY=mcproxy-k8s-rollout KUBE_CONTEXT=production \
  NAMESPACE=mcproxy ROLLOUT_TIMEOUT=10m \
  MCPROXY_MANIFEST=/path/to/rendered-mcproxy.yaml \
  PANEL_MANIFEST=/path/to/rendered-admin-panel.yaml \
  NETWORK_POLICY_MANIFEST=/path/to/rendered-network-policy.yaml \
  EGRESS_MANIFEST=/path/to/rendered-egress-policy.yaml \
  ./deploy/kubernetes/rollout-rehearsal.sh
```

`KUBE_CONTEXT` is optional for dry-run and, when omitted, the current kubectl
context is printed and used. Actual apply requires an explicit context as well
as the confirmation value above, which prevents an accidental apply to a
different cluster from a copied dry-run command.
