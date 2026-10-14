#!/usr/bin/env sh

set -eu

# Validate manifests against the target cluster, then optionally apply and wait
# for both workloads. Applying is deliberately opt-in so a local rehearsal
# cannot mutate a cluster by accident.
namespace="${NAMESPACE:-mcproxy}"
timeout="${ROLLOUT_TIMEOUT:-5m}"
context="${KUBE_CONTEXT:-}"
mcproxy_manifest="${MCPROXY_MANIFEST:-deploy/kubernetes/mcproxy.yaml}"
panel_manifest="${PANEL_MANIFEST:-deploy/kubernetes/admin-panel.yaml}"
network_policy="${NETWORK_POLICY_MANIFEST:-deploy/kubernetes/network-policy.yaml}"
egress_manifest="${EGRESS_MANIFEST:-}"
apply="${APPLY:-false}"
confirm="${CONFIRM_APPLY:-}"

kctl() {
	if [ -n "$context" ]; then
		kubectl --context "$context" "$@"
	else
		kubectl "$@"
	fi
}

require_file() {
	if [ ! -f "$1" ]; then
		echo "manifest not found: $1" >&2
		exit 2
	fi
}

case "$apply" in
	true|false) ;;
	*) echo "APPLY must be true or false" >&2; exit 2 ;;
esac

command -v kubectl >/dev/null 2>&1 || {
	echo "kubectl is required for Kubernetes rollout rehearsal" >&2
	exit 2
}

if [ "$apply" = true ] && [ "$confirm" != mcproxy-k8s-rollout ]; then
	echo "APPLY=true requires CONFIRM_APPLY=mcproxy-k8s-rollout" >&2
	exit 2
fi
if [ "$apply" = true ] && [ -z "$context" ]; then
	echo "APPLY=true requires KUBE_CONTEXT to name the target cluster" >&2
	exit 2
fi

require_file "$mcproxy_manifest"
require_file "$panel_manifest"
require_file "$network_policy"

manifests="$mcproxy_manifest $panel_manifest $network_policy"
if [ -n "$egress_manifest" ]; then
	require_file "$egress_manifest"
	# The example intentionally uses TEST-NET addresses. Never allow those
	# placeholders through an apply rehearsal.
	if grep -Eq '198\.51\.100\.|203\.0\.113\.|192\.0\.2\.' "$egress_manifest"; then
		echo "egress manifest still contains documentation CIDRs: $egress_manifest" >&2
		exit 2
	fi
	manifests="$manifests $egress_manifest"
fi

selected_context="$(kctl config current-context)"
echo "Validating Kubernetes manifests against namespace=$namespace context=$selected_context"
for manifest in $manifests; do
	echo "  dry-run: $manifest"
	kctl apply --server-side --dry-run=server -n "$namespace" -f "$manifest" >/dev/null
done

if [ "$apply" = false ]; then
	echo "Kubernetes rollout rehearsal OK (dry-run only; set APPLY=true to apply)"
	exit 0
fi

for manifest in $manifests; do
	echo "  apply: $manifest"
	kctl apply --server-side -n "$namespace" -f "$manifest" >/dev/null
done

kctl -n "$namespace" rollout status deployment/mcproxy --timeout="$timeout"
kctl -n "$namespace" rollout status deployment/mcproxy-admin-panel --timeout="$timeout"
kctl -n "$namespace" get pods -l app.kubernetes.io/part-of=mcproxy
echo "Kubernetes rollout rehearsal OK (applied and rolled out)"
