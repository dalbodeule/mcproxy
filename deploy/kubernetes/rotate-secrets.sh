#!/usr/bin/env sh

set -eu

namespace="${NAMESPACE:-mcproxy}"
target="${1:-all}"
timeout="${ROLLOUT_TIMEOUT:-5m}"

restart() {
	kubectl -n "$namespace" rollout restart "deployment/$1"
	kubectl -n "$namespace" rollout status "deployment/$1" --timeout="$timeout"
}

case "$target" in
	mcproxy)
		restart mcproxy
		;;
	panel)
		restart mcproxy-admin-panel
		;;
	all)
		restart mcproxy
		restart mcproxy-admin-panel
		;;
	*)
		echo "usage: $0 [mcproxy|panel|all]" >&2
		exit 2
		;;
esac
