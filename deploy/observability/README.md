# Prometheus observability

The checked-in files provide a starting scrape job and alert rules. They are not a replacement for the deployment's Prometheus and Alertmanager configuration.

## Validate before applying

Run the following from an environment that has the same Prometheus version used by the target cluster:

```sh
promtool check rules deploy/observability/alerts.yml
promtool check config deploy/observability/prometheus.yml
# or, with Docker and the repository-pinned Prometheus image:
make promtool-check
```

The repository's `make validate-deploy` checks YAML structure. `promtool` is still required to validate PromQL and Prometheus-specific semantics.

## Apply safely

1. Keep `/metrics` reachable only from the Prometheus network; do not expose it through the public panel ingress.
2. Replace `mcproxy:8080` in `prometheus.yml` with the internal service name or target address for the cluster.
3. Load `prometheus.yml` and `alerts.yml` through the Prometheus configuration mechanism used by the cluster.
4. Run the rule/config checks, reload Prometheus, and confirm the `mcproxy` target is `UP`.
5. Route warnings to an operator during an observation window before paging on them.

When `MCPROXY_ADMIN_ACTOR_SECRET` is enabled, `mcproxy_admin_actor_signature_invalid_total` counts malformed or invalid panel actor signatures. The `McproxyInvalidAdminActorSignature` rule is an investigation signal for a misconfigured shared secret or unauthorized requests; it does not block the API request because the request is still authenticated by the admin token.

## Threshold tuning

The connection-flood and latency thresholds are examples. Tune them from normal peak traffic and database behavior. The decision-block alerts use process-lifetime counters and require a minimum traffic rate to avoid reacting to tiny samples. `mcproxy:decision_block_ratio5m` compares the current ratio with `mcproxy:decision_block_ratio_baseline1h`; `McproxyAdaptiveDecisionBlockRatio` is alert-only and never mutates policy automatically.

`McproxyAuditWriteFailed` is intentionally critical: an audit persistence error can mean that an administrative or security event was not recorded. Investigate database connectivity, pool pressure, disk capacity, and migration state before treating the incident as recovered.

`McproxyDatabasePoolExhausted` is a critical signal for a saturated connection pool: every currently open connection is in use and callers are waiting. Treat it as an active capacity or slow-query incident, not as a reason to increase the pool blindly.

### A repeatable tuning window

Use a full normal peak window (at least 1 hour, preferably including the
known busiest period) before changing thresholds. Save the query results with
the deployment record, then keep the previous alert rule during a 24-hour
observation period.

Useful baseline queries are:

```promql
# Peak active players and Gate backends.
max_over_time(mcproxy_gate_players[1h])
max_over_time(mcproxy_gate_backends[1h])

# Peak evaluation latency in milliseconds.
1000 * rate(mcproxy_evaluation_latency_seconds_sum[1h])
  / clamp_min(rate(mcproxy_evaluation_latency_seconds_count[1h]), 1)

# Decision block ratio during the normal peak.
sum(increase(mcproxy_decision_blocks_total[1h]))
  / clamp_min(sum(increase(mcproxy_attempts_total[1h])), 1)

# Database pool pressure and wait rate.
max_over_time(mcproxy_db_in_use_connections[1h])
rate(mcproxy_db_wait_count_total[1h])
```

Set thresholds above the observed normal peak plus a documented margin, and
validate them with `promtool` before reload. A threshold change must not call
the policy mutation API automatically; policy changes remain an explicit,
audited operator action.

If a threshold or alert is changed, retain the old rule during the observation window and record the change with the normal deployment/audit process. Do not connect an unauthenticated Alertmanager webhook directly to the policy mutation API.
