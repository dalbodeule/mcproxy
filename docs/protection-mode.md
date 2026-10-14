# Protection mode runbook

mcproxy exposes evaluated connection attempts, allowed attempts, and process-lifetime decision blocks as Prometheus counters. The `McproxyHighDecisionBlockRatio` alert uses the process-lifetime counters so a restart cannot mix persisted historical blocks with fresh attempts. It is a signal for operator review; it is deliberately not an automatic global ban because a bad policy or an unavailable backend can produce the same ratio.

The example alert set also separates traffic volume from policy decisions: `McproxyConnectionFlood` reports more than 100 evaluated attempts per second for two minutes, and `McproxyServerConnectionFlood` identifies concentration on one configured backend. `McproxyEvaluationLatencyHigh`, `McproxySnapshotRefreshLatencyHigh`, and `McproxyDatabasePoolContention` distinguish slow policy reads, snapshot churn, and pool pressure from traffic volume. `McproxyDistributedFailClosed` is critical when Redis is unhealthy and `MCPROXY_REDIS_FAIL_CLOSED=true`. These thresholds are starting points only; tune them to the deployment's normal peak before routing them to paging.

`mcproxy:decision_block_ratio5m` and `mcproxy:decision_block_ratio_baseline1h` provide an adaptive, alert-only comparison. `McproxyAdaptiveDecisionBlockRatio` fires when the current ratio is at least 25 percentage points above the trailing one-hour baseline and above a 50% floor, with enough traffic to be meaningful. The baseline needs roughly one hour of Prometheus history and should be reviewed after policy or traffic-shape changes.

## First response

1. Confirm `mcproxy_gate_ready`, `mcproxy_gate_backends`, and backend health. A missing backend can make a normal deployment look like an attack.
2. Compare `rate(mcproxy_decision_blocks_total[5m])` with `rate(mcproxy_attempts_total[5m])` and inspect `mcproxy_blocks_by_reason_total`. Use `mcproxy_blocks_total` only as the persisted historical total.
3. Check the recent audit events for the affected server and source distribution. Do not rotate BungeeGuard tokens as an attack response; token rotation requires an immediate backend plugin reload.
4. If the event is a false positive, raise or disable the affected server/global threshold, Geo policy, or rule and record the change in the audit log.
5. If the event is confirmed abuse, use the narrowest server/IP/CIDR/name rule possible, keep the backend firewall restricted to mcproxy, and monitor the ratio after the change.

## Optional automation boundary

Alertmanager may notify an operator, incident system, or an authenticated deployment workflow. Any workflow that changes policy must require an explicit allowlisted action, include the target server ID, preserve the existing policy for rollback, and authenticate with a short-lived administrative credential. Do not connect an unauthenticated webhook directly to the policy mutation API.

The panel displays the current decision block ratio when the process has evaluated attempts since startup. Prometheus remains the source of truth for historical and multi-instance ratios.
