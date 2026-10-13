# Kubernetes deployment templates

These manifests assume PostgreSQL and Redis are managed services or are deployed separately. Create the referenced Secrets before applying the manifests, and replace the example image names with immutable registry tags or digests.

```sh
kubectl -n mcproxy create namespace mcproxy
kubectl -n mcproxy create secret generic mcproxy-runtime \
  --from-literal=admin-token='replace-me' \
  --from-literal=postgres-dsn='postgres://user:password@postgres.example/mcproxy?sslmode=verify-full' \
  --from-literal=redis-password='replace-me'
kubectl -n mcproxy create secret generic mcproxy-panel \
  --from-literal=admin-token='replace-me' \
  --from-literal=username='admin' \
  --from-literal=password='replace-me' \
  --from-literal=session-secret='replace-with-at-least-32-random-characters'
kubectl apply -f deploy/kubernetes/mcproxy.yaml
kubectl apply -f deploy/kubernetes/admin-panel.yaml
kubectl apply -f deploy/kubernetes/network-policy.yaml
```

The mcproxy image starts with a tiny root init process only to prepare the data directory, then `exec`s the application as UID 65532. The template mounts a `PersistentVolumeClaim` at `/data`; configure a suitable StorageClass and size for the target cluster. Do not override the entrypoint or force `runAsUser: 65532` unless the image is rebuilt with an equivalent volume-permission strategy. The admin-panel image already runs as the non-root `node` user.

Both workloads disable automatic ServiceAccount token mounting. The separate admin panel has two replicas and a `PodDisruptionBudget` with `minAvailable: 1`; keep at least two schedulable nodes if voluntary-disruption availability is required.

Keep the HTTP service and `/metrics` internal. Expose only the Minecraft TCP service and the panel through a TLS-terminating ingress or load balancer. The example NetworkPolicies assume an `ingress-nginx` controller and Prometheus pods labelled `prometheus: "true"`; adjust those selectors for the target cluster. Configure managed-database backups and an external Secret Manager before production.

## External Secret Manager

`external-secrets.example.yaml` is an optional External Secrets Operator template. It is not included in the default `kubectl apply` commands because the `ClusterSecretStore` and remote key names are provider-specific. After installing the operator, create a provider-specific store, replace the store name and `remoteRef.key` values, then apply it:

```sh
kubectl apply -f deploy/kubernetes/external-secrets.example.yaml
kubectl -n mcproxy get externalsecret mcproxy-runtime mcproxy-panel
kubectl -n mcproxy rollout status deployment/mcproxy
kubectl -n mcproxy rollout status deployment/mcproxy-admin-panel
```

The generated Secret names and keys match the two workload manifests. Provider credentials are not stored in this repository. Rotate remote values through the secret manager and let the operator refresh the Kubernetes Secrets.

The example NetworkPolicy restricts ingress only. Add provider-specific egress rules before enabling default-deny egress: mcproxy needs PostgreSQL, Redis, Minecraft upstreams, and optional GeoIP access, while the panel needs the mcproxy HTTP service. A blanket egress deny without those destinations will make the deployment unavailable.
