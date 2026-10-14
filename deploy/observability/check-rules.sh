#!/usr/bin/env sh
set -eu

rules=${1:-deploy/observability/alerts.yml}
[ -r "$rules" ] || { echo "Prometheus rules file is not readable: $rules" >&2; exit 1; }

for required in \
	'mcproxy:decision_block_ratio5m' \
	'mcproxy:decision_block_ratio_baseline1h' \
	'McproxyHighDecisionBlockRatio' \
	'McproxyAdaptiveDecisionBlockRatio' \
	'McproxyDistributedFailClosed' \
	'McproxyInvalidAdminActorSignature' \
	'McproxyAuditWriteFailed'; do
	if ! grep -F "$required" "$rules" >/dev/null; then
		echo "missing required Prometheus rule: $required" >&2
		exit 1
	fi
done

echo "Prometheus rule inventory OK: $rules"
