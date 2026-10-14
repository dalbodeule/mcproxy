package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"mcproxy/internal/buildinfo"
	"mcproxy/internal/config"
	"mcproxy/internal/ent"
	"mcproxy/internal/geo"
	"mcproxy/internal/observability"
	"mcproxy/internal/store"
)

type Dependencies struct {
	Store *store.Store
	Geo   *geo.Service
	// GateReady is optional for API-only deployments. When provided, the
	// readiness endpoint also reflects whether the Gate data plane is running.
	GateReady func() bool
	// GateStats returns the current connected-player and registered-backend
	// counts. It is optional for API-only deployments.
	GateStats func() (players, backends int)
	// GateBackendStatus reports whether a configured backend is registered in
	// Gate and whether its current forwarding token has been applied.
	GateBackendStatus func(serverID int, expectedToken string) (registered, tokenApplied bool)
}

type adminActorContextKey struct{}

const maxJSONBodyBytes int64 = 1 << 20
const maxAuditOffset = 1_000_000

var invalidAdminActorSignatures atomic.Uint64

func adminActor(ctx context.Context) string {
	if actor, ok := ctx.Value(adminActorContextKey{}).(string); ok && actor != "" {
		return actor
	}
	return "admin-api"
}

func serverHealthPayload(id int, status string, healthy bool, item *store.ServerDTO, deps Dependencies) map[string]any {
	payload := map[string]any{"server_id": id, "status": status, "healthy": healthy}
	if deps.GateBackendStatus != nil {
		registered, tokenApplied := deps.GateBackendStatus(id, item.BungeeGuardAllowedToken)
		payload["gate_registered"] = registered
		payload["gate_token_applied"] = tokenApplied
	}
	return payload
}

func NewRouter(cfg config.Config, deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// Basic hardening middlewares
	middlewares := []func(http.Handler) http.Handler{
		middleware.RequestID,
		middleware.Recoverer,
		middleware.Timeout(60 * time.Second),
		observability.RequestLogger,
		securityHeaders,
		cors(cfg.AdminAllowedOrigins),
	}
	if cfg.TrustProxyHeaders && len(cfg.TrustedProxyCIDRs) > 0 {
		// Only trust forwarded client IP headers when the immediate peer is in
		// an explicitly configured proxy network. chi.RealIP trusts headers from
		// every peer, which is unsafe when this listener is internet-facing.
		middlewares = append([]func(http.Handler) http.Handler{trustedProxyRealIP(cfg.TrustedProxyCIDRs)}, middlewares...)
	}
	r.Use(middlewares...)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339Nano),
		})
	})
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		if deps.Store == nil || deps.Store.Health(r.Context()) != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
			return
		}
		distributed := deps.Store.DistributedMetrics()
		if distributed.Enabled && distributed.FailClosed {
			if err := deps.Store.CheckDistributed(r.Context()); err != nil {
				observability.Warn("ready_distributed_check_failed", "error", err)
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "distributed_not_ready"})
				return
			}
			distributed = deps.Store.DistributedMetrics()
		}
		if distributed.Enabled && distributed.FailClosed && !distributed.Healthy {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "distributed_not_ready"})
			return
		}
		if deps.GateReady != nil && !deps.GateReady() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "gate_not_ready"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
	})
	r.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if deps.Store == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		st, err := deps.Store.Stats(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = io.WriteString(w, "# HELP mcproxy_build_info Build metadata for the running mcproxy process.\n# TYPE mcproxy_build_info gauge\n")
		_, _ = io.WriteString(w, "mcproxy_build_info{version=\""+promEscape(buildinfo.Version)+"\",commit=\""+promEscape(buildinfo.Commit)+"\",build_time=\""+promEscape(buildinfo.BuildTime)+"\",go_version=\""+promEscape(runtime.Version())+"\"} 1\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_admin_actor_signature_invalid_total Invalid panel actor signatures observed while actor signing is enabled.\n# TYPE mcproxy_admin_actor_signature_invalid_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_admin_actor_signature_invalid_total "+strconv.FormatUint(invalidAdminActorSignatures.Load(), 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_uptime_seconds Process uptime in seconds.\n# TYPE mcproxy_uptime_seconds gauge\n")
		_, _ = io.WriteString(w, "mcproxy_uptime_seconds "+strconv.FormatInt(st.UptimeSeconds, 10)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_servers gauge\nmcproxy_servers "+strconv.Itoa(st.Servers)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_enabled_servers gauge\nmcproxy_enabled_servers "+strconv.Itoa(st.EnabledServers)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_policies gauge\nmcproxy_policies "+strconv.Itoa(st.Policies)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_rules gauge\nmcproxy_rules "+strconv.Itoa(st.Rules)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_counters gauge\nmcproxy_counters "+strconv.Itoa(st.Counters)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_blocks_total counter\nmcproxy_blocks_total "+strconv.FormatInt(st.Blocks, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_attempts_total Valid connection attempts evaluated by the policy engine.\n# TYPE mcproxy_attempts_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_attempts_total "+strconv.FormatInt(st.Attempts, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_allowed_total Connection attempts allowed by the policy engine.\n# TYPE mcproxy_allowed_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_allowed_total "+strconv.FormatInt(st.Allowed, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_decision_blocks_total Connection attempts blocked by the policy engine since process start.\n# TYPE mcproxy_decision_blocks_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_decision_blocks_total "+strconv.FormatInt(st.DecisionBlocks, 10)+"\n")
		for reason, count := range st.BlockReasons {
			_, _ = io.WriteString(w, "mcproxy_blocks_by_reason_total{reason=\""+promEscape(reason)+"\"} "+strconv.FormatInt(count, 10)+"\n")
		}
		evaluation := deps.Store.EvaluationMetrics()
		_, _ = io.WriteString(w, "# HELP mcproxy_evaluation_latency_seconds_total Total policy evaluation time since process start.\n# TYPE mcproxy_evaluation_latency_seconds_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_evaluation_latency_seconds_total "+strconv.FormatFloat(evaluation.DurationSeconds, 'f', 6, 64)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_evaluation_latency_seconds_count Number of valid policy evaluations observed.\n# TYPE mcproxy_evaluation_latency_seconds_count counter\n")
		_, _ = io.WriteString(w, "mcproxy_evaluation_latency_seconds_count "+strconv.FormatInt(evaluation.DurationCount, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_server_attempts_total Valid policy evaluations by configured server.\n# TYPE mcproxy_server_attempts_total counter\n")
		for serverID, count := range evaluation.ServerAttempts {
			_, _ = io.WriteString(w, "mcproxy_server_attempts_total{server_id=\""+strconv.Itoa(serverID)+"\"} "+strconv.FormatInt(count, 10)+"\n")
		}
		_, _ = io.WriteString(w, "# HELP mcproxy_snapshot_refresh_latency_seconds_total Total snapshot refresh time since process start.\n# TYPE mcproxy_snapshot_refresh_latency_seconds_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_snapshot_refresh_latency_seconds_total "+strconv.FormatFloat(evaluation.SnapshotDurationSeconds, 'f', 6, 64)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_snapshot_refreshes_total Successful snapshot refreshes since process start.\n# TYPE mcproxy_snapshot_refreshes_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_snapshot_refreshes_total "+strconv.FormatUint(evaluation.SnapshotRefreshes, 10)+"\n")
		db := deps.Store.DatabaseMetrics()
		storeLatency := deps.Store.StoreLatencyMetrics()
		_, _ = io.WriteString(w, "# HELP mcproxy_store_stats_latency_seconds_total Total duration of database-backed store stats operations since process start.\n# TYPE mcproxy_store_stats_latency_seconds_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_store_stats_latency_seconds_total "+strconv.FormatFloat(storeLatency.StatsDurationSeconds, 'f', 6, 64)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_store_stats_latency_seconds_count Number of database-backed store stats operations observed.\n# TYPE mcproxy_store_stats_latency_seconds_count counter\n")
		_, _ = io.WriteString(w, "mcproxy_store_stats_latency_seconds_count "+strconv.FormatInt(storeLatency.StatsDurationCount, 10)+"\n")
		audit := deps.Store.AuditMetrics()
		_, _ = io.WriteString(w, "# HELP mcproxy_audit_write_errors_total Audit persistence failures since process start.\n# TYPE mcproxy_audit_write_errors_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_audit_write_errors_total "+strconv.FormatInt(audit.WriteErrors, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_db_open_connections Current database pool open connections.\n# TYPE mcproxy_db_open_connections gauge\n")
		_, _ = io.WriteString(w, "mcproxy_db_open_connections "+strconv.Itoa(db.OpenConnections)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_db_in_use_connections Current database pool connections in use.\n# TYPE mcproxy_db_in_use_connections gauge\n")
		_, _ = io.WriteString(w, "mcproxy_db_in_use_connections "+strconv.Itoa(db.InUse)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_db_idle_connections Current database pool idle connections.\n# TYPE mcproxy_db_idle_connections gauge\n")
		_, _ = io.WriteString(w, "mcproxy_db_idle_connections "+strconv.Itoa(db.Idle)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_db_wait_count_total Database pool waits since process start.\n# TYPE mcproxy_db_wait_count_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_db_wait_count_total "+strconv.FormatInt(db.WaitCount, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_db_wait_duration_seconds_total Database pool wait duration since process start.\n# TYPE mcproxy_db_wait_duration_seconds_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_db_wait_duration_seconds_total "+strconv.FormatFloat(db.WaitDuration.Seconds(), 'f', 6, 64)+"\n")
		cache := deps.Store.CacheMetrics()
		_, _ = io.WriteString(w, "# HELP mcproxy_snapshot_cache_hits_total Snapshot cache hits.\n# TYPE mcproxy_snapshot_cache_hits_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_snapshot_cache_hits_total "+strconv.FormatUint(cache.Hits, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_snapshot_cache_misses_total Snapshot cache refreshes or misses.\n# TYPE mcproxy_snapshot_cache_misses_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_snapshot_cache_misses_total "+strconv.FormatUint(cache.Misses, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_snapshot_cache_age_seconds Age of the current policy/rule snapshot.\n# TYPE mcproxy_snapshot_cache_age_seconds gauge\n")
		_, _ = io.WriteString(w, "mcproxy_snapshot_cache_age_seconds "+strconv.FormatFloat(cache.AgeSeconds, 'f', 3, 64)+"\n")
		distributed := deps.Store.DistributedMetrics()
		distributedEnabled := 0
		if distributed.Enabled {
			distributedEnabled = 1
		}
		distributedFailClosed := 0
		if distributed.FailClosed {
			distributedFailClosed = 1
		}
		distributedHealthy := 0
		if distributed.Healthy {
			distributedHealthy = 1
		}
		_, _ = io.WriteString(w, "# HELP mcproxy_distributed_enabled Whether Redis-backed distributed coordination is configured.\n# TYPE mcproxy_distributed_enabled gauge\n")
		_, _ = io.WriteString(w, "mcproxy_distributed_enabled "+strconv.Itoa(distributedEnabled)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_distributed_healthy Whether Redis-backed coordination is currently healthy. Disabled coordination is reported healthy.\n# TYPE mcproxy_distributed_healthy gauge\n")
		_, _ = io.WriteString(w, "mcproxy_distributed_healthy "+strconv.Itoa(distributedHealthy)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_distributed_fail_closed Whether Redis counter failures deny new evaluations.\n# TYPE mcproxy_distributed_fail_closed gauge\n")
		_, _ = io.WriteString(w, "mcproxy_distributed_fail_closed "+strconv.Itoa(distributedFailClosed)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_distributed_errors_total Redis coordination errors since process start.\n# TYPE mcproxy_distributed_errors_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_distributed_errors_total "+strconv.FormatInt(distributed.Errors, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_distributed_fallbacks_total Requests that fell back to local coordination since process start.\n# TYPE mcproxy_distributed_fallbacks_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_distributed_fallbacks_total "+strconv.FormatInt(distributed.Fallbacks, 10)+"\n")
		_, _ = io.WriteString(w, "# HELP mcproxy_distributed_publish_errors_total Redis cache invalidation publish errors since process start.\n# TYPE mcproxy_distributed_publish_errors_total counter\n")
		_, _ = io.WriteString(w, "mcproxy_distributed_publish_errors_total "+strconv.FormatInt(distributed.PublishErrors, 10)+"\n")
		if deps.GateReady != nil {
			gateReady := 0
			if deps.GateReady() {
				gateReady = 1
			}
			_, _ = io.WriteString(w, "# HELP mcproxy_gate_ready Whether the Gate data plane is running.\n# TYPE mcproxy_gate_ready gauge\n")
			_, _ = io.WriteString(w, "mcproxy_gate_ready "+strconv.Itoa(gateReady)+"\n")
		}
		if deps.GateStats != nil {
			players, backends := deps.GateStats()
			_, _ = io.WriteString(w, "# HELP mcproxy_gate_players Current connected players on the Gate data plane.\n# TYPE mcproxy_gate_players gauge\n")
			_, _ = io.WriteString(w, "mcproxy_gate_players "+strconv.Itoa(players)+"\n")
			_, _ = io.WriteString(w, "# HELP mcproxy_gate_backends Registered backend servers on the Gate data plane.\n# TYPE mcproxy_gate_backends gauge\n")
			_, _ = io.WriteString(w, "mcproxy_gate_backends "+strconv.Itoa(backends)+"\n")
		}
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(middleware.ThrottleBacklog(cfg.AdminThrottle, cfg.AdminThrottle*2, 30*time.Second))
		api.Use(limitBody(1 << 20))
		api.Use(adminAuth(cfg.AdminToken, cfg.AdminActorSecret))

		api.Get("/stats", func(w http.ResponseWriter, r *http.Request) {
			st, err := deps.Store.Stats(r.Context())
			if err != nil {
				// Avoid leaking internal errors to clients
				observability.Error("stats_handler_error", "error", err)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
				return
			}
			writeJSON(w, http.StatusOK, st)
		})
		api.Get("/audits", func(w http.ResponseWriter, r *http.Request) {
			limit, offset := queryPage(r)
			serverIDs, ok := queryIntList(w, r, "server_id")
			if !ok {
				return
			}
			items, err := deps.Store.ListAudits(r.Context(), limit+1, offset, r.URL.Query().Get("event"), r.URL.Query().Get("actor"), serverIDs)
			if handleStoreError(w, "list audits", err) {
				return
			}
			hasMore := len(items) > limit
			if hasMore {
				items = items[:limit]
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": hasMore, "limit": limit, "offset": offset})
		})

		api.Route("/servers", func(sr chi.Router) {
			sr.Get("/", func(w http.ResponseWriter, r *http.Request) {
				items, err := deps.Store.ListServers(r.Context())
				if err != nil {
					internalError(w, "list servers", err)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"items": items})
			})
			sr.Post("/", func(w http.ResponseWriter, r *http.Request) {
				var in store.ServerInput
				if err := decodeJSON(r, &in); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				if in.Name == "" || in.Upstream == "" {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": "name and upstream are required"})
					return
				}
				item, err := deps.Store.CreateServer(r.Context(), in)
				if err != nil {
					internalError(w, "create server", err)
					return
				}
				recordAdminAudit(r.Context(), deps.Store, "server.create", &item.ID, map[string]any{"server_id": item.ID, "name": item.Name, "upstream": item.Upstream, "forwarding_mode": item.ForwardingMode})
				writeJSON(w, http.StatusCreated, item)
			})
			sr.Route("/{serverID}", func(sr chi.Router) {
				sr.Get("/", func(w http.ResponseWriter, r *http.Request) {
					id, ok := pathInt(w, r, "serverID")
					if !ok {
						return
					}
					item, err := deps.Store.GetServer(r.Context(), id)
					if handleStoreError(w, "get server", err) {
						return
					}
					writeJSON(w, http.StatusOK, item)
				})
				sr.Get("/health", func(w http.ResponseWriter, r *http.Request) {
					id, ok := pathInt(w, r, "serverID")
					if !ok {
						return
					}
					item, err := deps.Store.GetServer(r.Context(), id)
					if handleStoreError(w, "get server for health", err) {
						return
					}
					if !item.Enabled {
						writeJSON(w, http.StatusOK, serverHealthPayload(id, "disabled", false, item, deps))
						return
					}
					dialer := net.Dialer{Timeout: 800 * time.Millisecond}
					conn, dialErr := dialer.DialContext(r.Context(), "tcp", item.Upstream)
					if dialErr != nil {
						writeJSON(w, http.StatusOK, serverHealthPayload(id, "unreachable", false, item, deps))
						return
					}
					_ = conn.Close()
					writeJSON(w, http.StatusOK, serverHealthPayload(id, "healthy", true, item, deps))
				})
				sr.Put("/", func(w http.ResponseWriter, r *http.Request) {
					id, ok := pathInt(w, r, "serverID")
					if !ok {
						return
					}
					var in store.ServerInput
					if err := decodeJSON(r, &in); err != nil {
						writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
						return
					}
					item, err := deps.Store.UpdateServer(r.Context(), id, in)
					if handleStoreError(w, "update server", err) {
						return
					}
					recordAdminAudit(r.Context(), deps.Store, "server.update", &id, map[string]any{"server_id": id, "name": item.Name, "upstream": item.Upstream, "forwarding_mode": item.ForwardingMode, "enabled": item.Enabled})
					writeJSON(w, http.StatusOK, item)
				})
				sr.Post("/bungeeguard-token", func(w http.ResponseWriter, r *http.Request) {
					id, ok := pathInt(w, r, "serverID")
					if !ok {
						return
					}
					item, err := deps.Store.RotateBungeeGuardToken(r.Context(), id)
					if handleStoreError(w, "rotate bungeeguard token", err) {
						return
					}
					recordAdminAudit(r.Context(), deps.Store, "server.bungeeguard_token.rotate", &id, map[string]any{"server_id": id})
					writeJSON(w, http.StatusOK, item)
				})
				sr.Delete("/", func(w http.ResponseWriter, r *http.Request) {
					id, ok := pathInt(w, r, "serverID")
					if !ok {
						return
					}
					err := deps.Store.DeleteServer(r.Context(), id)
					if handleStoreError(w, "delete server", err) {
						return
					}
					// DeleteServer clears the optional audit->server relation before
					// removing the server. Keep the id in details, but do not enqueue
					// a dangling foreign-key reference after the deletion commits.
					recordAdminAudit(r.Context(), deps.Store, "server.delete", nil, map[string]any{"server_id": id})
					w.WriteHeader(http.StatusNoContent)
				})
			})
		})

		api.Route("/policy", func(pr chi.Router) {
			pr.Get("/global", func(w http.ResponseWriter, r *http.Request) {
				item, err := deps.Store.GetGlobalPolicy(r.Context())
				if handleStoreError(w, "get global policy", err) {
					return
				}
				writeJSON(w, http.StatusOK, item)
			})
			pr.Put("/global", func(w http.ResponseWriter, r *http.Request) {
				var in store.PolicyInput
				if err := decodeJSON(r, &in); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				item, err := deps.Store.UpsertGlobalPolicy(r.Context(), in)
				if handleStoreError(w, "upsert global policy", err) {
					return
				}
				recordAdminAudit(r.Context(), deps.Store, "policy.update", nil, map[string]any{"scope": "global"})
				writeJSON(w, http.StatusOK, item)
			})
			pr.Delete("/global", func(w http.ResponseWriter, r *http.Request) {
				if err := deps.Store.DeletePolicy(r.Context(), nil); handleStoreError(w, "delete global policy", err) {
					return
				}
				recordAdminAudit(r.Context(), deps.Store, "policy.reset", nil, map[string]any{"scope": "global"})
				w.WriteHeader(http.StatusNoContent)
			})
			pr.Get("/servers/{serverID}", func(w http.ResponseWriter, r *http.Request) {
				id, ok := pathInt(w, r, "serverID")
				if !ok {
					return
				}
				item, err := deps.Store.GetServerPolicy(r.Context(), id)
				if handleStoreError(w, "get server policy", err) {
					return
				}
				if item == nil {
					writeJSON(w, http.StatusOK, map[string]any{"scope": "server", "server_id": id, "policy": nil, "inherited": true})
					return
				}
				writeJSON(w, http.StatusOK, item)
			})
			pr.Put("/servers/{serverID}", func(w http.ResponseWriter, r *http.Request) {
				id, ok := pathInt(w, r, "serverID")
				if !ok {
					return
				}
				var in store.PolicyInput
				if err := decodeJSON(r, &in); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				item, err := deps.Store.UpsertServerPolicy(r.Context(), id, in)
				if handleStoreError(w, "upsert server policy", err) {
					return
				}
				recordAdminAudit(r.Context(), deps.Store, "policy.update", &id, map[string]any{"scope": "server"})
				writeJSON(w, http.StatusOK, item)
			})
			pr.Delete("/servers/{serverID}", func(w http.ResponseWriter, r *http.Request) {
				id, ok := pathInt(w, r, "serverID")
				if !ok {
					return
				}
				if err := deps.Store.DeletePolicy(r.Context(), &id); handleStoreError(w, "delete server policy", err) {
					return
				}
				recordAdminAudit(r.Context(), deps.Store, "policy.reset", &id, map[string]any{"scope": "server"})
				w.WriteHeader(http.StatusNoContent)
			})
			pr.Get("/geo", func(w http.ResponseWriter, r *http.Request) {
				serverID, ok := optionalQueryInt(w, r, "server_id")
				if !ok {
					return
				}
				item, err := deps.Store.GetGeoPolicy(r.Context(), serverID)
				if handleStoreError(w, "get geo policy", err) {
					return
				}
				if item == nil {
					writeJSON(w, http.StatusOK, map[string]any{"mode": "disabled", "countries": []string{}, "scope": "global"})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"mode": item.GeoMode, "countries": item.GeoList, "scope": item.Scope})
			})
			pr.Put("/geo", func(w http.ResponseWriter, r *http.Request) {
				serverID, ok := optionalQueryInt(w, r, "server_id")
				if !ok {
					return
				}
				var in struct {
					Mode      string   `json:"mode"`
					Countries []string `json:"countries"`
				}
				if err := decodeJSON(r, &in); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				item, err := deps.Store.UpsertGeoPolicy(r.Context(), serverID, in.Mode, in.Countries)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				var auditServerID *int
				if serverID != nil {
					auditServerID = serverID
				}
				recordAdminAudit(r.Context(), deps.Store, "geo.update", auditServerID, map[string]any{"mode": item.GeoMode, "countries": item.GeoList})
				writeJSON(w, http.StatusOK, map[string]any{"mode": item.GeoMode, "countries": item.GeoList, "scope": item.Scope})
			})
			pr.Delete("/geo", func(w http.ResponseWriter, r *http.Request) {
				serverID, ok := optionalQueryInt(w, r, "server_id")
				if !ok {
					return
				}
				if err := deps.Store.ResetGeoPolicy(r.Context(), serverID); handleStoreError(w, "reset geo policy", err) {
					return
				}
				recordAdminAudit(r.Context(), deps.Store, "geo.reset", serverID, map[string]any{"mode": "disabled"})
				w.WriteHeader(http.StatusNoContent)
			})
		})

		api.Route("/rules", func(rr chi.Router) {
			rr.Get("/", func(w http.ResponseWriter, r *http.Request) {
				limit, offset := queryPage(r)
				serverID, ok := optionalQueryInt(w, r, "server_id")
				if !ok {
					return
				}
				items, hasMore, err := deps.Store.ListRulesPage(r.Context(), serverID, limit+1, offset)
				if handleStoreError(w, "list rules", err) {
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"items": items, "has_more": hasMore, "limit": limit, "offset": offset})
			})
			rr.Post("/", func(w http.ResponseWriter, r *http.Request) {
				var in store.RuleInput
				if err := decodeJSON(r, &in); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				item, err := deps.Store.CreateRule(r.Context(), in)
				if err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
					return
				}
				recordAdminAudit(r.Context(), deps.Store, "rule.create", item.ServerID, map[string]any{"kind": item.Kind, "target": item.Target})
				writeJSON(w, http.StatusCreated, item)
			})
			rr.Delete("/{ruleID}", func(w http.ResponseWriter, r *http.Request) {
				id, ok := pathInt(w, r, "ruleID")
				if !ok {
					return
				}
				item, err := deps.Store.DeleteRule(r.Context(), id)
				if handleStoreError(w, "delete rule", err) {
					return
				}
				recordAdminAudit(r.Context(), deps.Store, "rule.delete", item.ServerID, map[string]any{"rule_id": id, "kind": item.Kind, "target": item.Target})
				w.WriteHeader(http.StatusNoContent)
			})
		})

		api.Post("/evaluate", func(w http.ResponseWriter, r *http.Request) {
			var in store.EvaluateAttemptInput
			if err := decodeJSON(r, &in); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			if strings.TrimSpace(in.Country) == "" && deps.Geo != nil {
				if ip := net.ParseIP(strings.TrimSpace(in.IP)); ip != nil {
					in.Country = deps.Geo.CountryCode(ip)
					if strings.TrimSpace(in.ASN) == "" {
						in.ASN = deps.Geo.ASN(ip)
					}
				}
			} else if strings.TrimSpace(in.ASN) == "" && deps.Geo != nil {
				if ip := net.ParseIP(strings.TrimSpace(in.IP)); ip != nil {
					in.ASN = deps.Geo.ASN(ip)
				}
			}
			item, err := deps.Store.EvaluateAttempt(r.Context(), in)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, item)
		})
	})

	return r
}

func adminAuth(token, actorSecret string) func(http.Handler) http.Handler {
	var failures = authFailureStore{items: make(map[string]authFailure)}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				observability.Warn("admin_api_disabled", "path", r.URL.Path)
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "admin api disabled: MCPROXY_ADMIN_TOKEN is not configured"})
				return
			}

			provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if provided == "" {
				provided = strings.TrimSpace(r.Header.Get("X-API-Token"))
			}

			// Compare fixed-size digests so a malformed token cannot take the
			// shorter length path in subtle.ConstantTimeCompare.
			providedDigest := sha256.Sum256([]byte(provided))
			expectedDigest := sha256.Sum256([]byte(token))
			if subtle.ConstantTimeCompare(providedDigest[:], expectedDigest[:]) != 1 {
				remoteIP := requestIP(r)
				if !failures.allow(remoteIP, time.Now()) {
					w.Header().Set("Retry-After", "60")
					writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many authentication failures"})
					return
				}
				observability.Warn("admin_auth_failed", "path", r.URL.Path, "remote_ip", remoteIP)
				w.Header().Set("WWW-Authenticate", "Bearer")
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
				return
			}
			failures.reset(requestIP(r))

			rawActor := r.Header.Get("X-Admin-Actor")
			rawActorSignature := r.Header.Get("X-Admin-Actor-Signature")
			actor := verifiedAdminActor(actorSecret, rawActor, rawActorSignature)
			if actorSecret != "" && (strings.TrimSpace(rawActor) != "" || strings.TrimSpace(rawActorSignature) != "") && actor == "" {
				invalidAdminActorSignatures.Add(1)
				// Do not log the actor or signature: either may be attacker-controlled
				// and the signature is sensitive authentication material.
				observability.Warn("admin_actor_signature_invalid", "path", r.URL.Path)
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), adminActorContextKey{}, actor)))
		})
	}
}

func verifiedAdminActor(secret, rawActor, rawSignature string) string {
	if secret == "" {
		return ""
	}
	actor := strings.TrimSpace(rawActor)
	if actor == "" || len(actor) > 128 {
		return ""
	}
	for _, char := range actor {
		if char < 0x20 || char == 0x7f {
			return ""
		}
	}
	provided, err := hex.DecodeString(strings.TrimSpace(rawSignature))
	if err != nil || len(provided) != sha256.Size {
		return ""
	}
	digest := hmac.New(sha256.New, []byte(secret))
	_, _ = digest.Write([]byte(actor))
	if !hmac.Equal(provided, digest.Sum(nil)) {
		return ""
	}
	return actor
}

type authFailure struct {
	started  time.Time
	failures int
}

type authFailureStore struct {
	sync.Mutex
	items map[string]authFailure
}

func (f *authFailureStore) allow(ip string, now time.Time) bool {
	f.Lock()
	defer f.Unlock()
	item := f.items[ip]
	if item.started.IsZero() || now.Sub(item.started) >= time.Minute {
		item = authFailure{started: now}
	}
	item.failures++
	f.items[ip] = item
	if len(f.items) > 10000 {
		for key := range f.items {
			delete(f.items, key)
			break
		}
	}
	return item.failures <= 20
}

func (f *authFailureStore) reset(ip string) {
	f.Lock()
	delete(f.items, ip)
	f.Unlock()
}

func requestIP(r *http.Request) string {
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return remote
}

func trustedProxyRealIP(cidrs []string) func(http.Handler) http.Handler {
	trusted := make([]*net.IPNet, 0, len(cidrs))
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err == nil {
			trusted = append(trusted, network)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer := requestIP(r)
			peerIP := net.ParseIP(peer)
			if peerIP == nil || !ipInNetworks(peerIP, trusted) {
				next.ServeHTTP(w, r)
				return
			}
			forwarded := forwardedClientIP(r)
			if forwarded != "" {
				_, port, _ := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
				if port == "" {
					r.RemoteAddr = forwarded
				} else {
					r.RemoteAddr = net.JoinHostPort(forwarded, port)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func forwardedClientIP(r *http.Request) string {
	if values := strings.Split(r.Header.Get("X-Forwarded-For"), ","); len(values) > 0 {
		if ip := net.ParseIP(strings.TrimSpace(values[0])); ip != nil {
			return ip.String()
		}
	}
	if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
		return ip.String()
	}
	return ""
}

func ipInNetworks(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func cors(allowed []string) func(http.Handler) http.Handler {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, origin := range allowed {
		allowedSet[origin] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := allowedSet[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Token, X-Admin-Actor, X-Admin-Actor-Signature")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				if origin == "" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if _, ok := allowedSet[origin]; !ok {
					writeJSON(w, http.StatusForbidden, map[string]any{"error": "origin not allowed"})
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func limitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Best-effort logging only; headers already sent
		observability.Error("write_json_encode_error", "error", err)
	}
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	if r.ContentLength > maxJSONBodyBytes {
		return fmt.Errorf("request body too large (maximum %d bytes)", maxJSONBodyBytes)
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONBodyBytes+1))
	if err != nil {
		return err
	}
	if int64(len(body)) > maxJSONBodyBytes {
		return fmt.Errorf("request body too large (maximum %d bytes)", maxJSONBodyBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func pathInt(w http.ResponseWriter, r *http.Request, name string) (int, bool) {
	v := chi.URLParam(r, name)
	id, err := strconv.Atoi(v)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid path parameter: " + name})
		return 0, false
	}
	return id, true
}

func optionalQueryInt(w http.ResponseWriter, r *http.Request, name string) (*int, bool) {
	v := strings.TrimSpace(r.URL.Query().Get(name))
	if v == "" {
		return nil, true
	}
	id, err := strconv.Atoi(v)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid query parameter: " + name})
		return nil, false
	}
	return &id, true
}

func queryIntList(w http.ResponseWriter, r *http.Request, name string) ([]int, bool) {
	const maxIDs = 256
	values := r.URL.Query()[name]
	if len(values) == 0 {
		return nil, true
	}
	result := make([]int, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if len(result) >= maxIDs {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "too many values for query parameter: " + name})
				return nil, false
			}
			id, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || id <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid query parameter: " + name})
				return nil, false
			}
			result = append(result, id)
		}
	}
	return result, true
}

func queryPage(r *http.Request) (limit, offset int) {
	limit, offset = 50, 0
	if value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit"))); err == nil && value > 0 {
		limit = value
	}
	if limit > 100 {
		limit = 100
	}
	if value, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset"))); err == nil && value >= 0 {
		offset = value
	}
	if offset > maxAuditOffset {
		offset = maxAuditOffset
	}
	return limit, offset
}

func promEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, "\r", `\r`)
}

func recordAdminAudit(ctx context.Context, st *store.Store, event string, serverID *int, details map[string]any) {
	if st == nil {
		return
	}
	if err := st.RecordAudit(ctx, adminActor(ctx), serverID, event, details); err != nil {
		observability.Warn("admin_audit_write_failed", "event", event, "error", err)
	}
}

func handleStoreError(w http.ResponseWriter, action string, err error) bool {
	if err == nil {
		return false
	}
	if ent.IsNotFound(err) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "resource not found"})
		return true
	}
	internalError(w, action, err)
	return true
}

func internalError(w http.ResponseWriter, action string, err error) {
	observability.Error("internal_error", "action", action, "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
}
