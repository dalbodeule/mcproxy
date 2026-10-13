package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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
	if cfg.TrustProxyHeaders {
		// Only enable this when a trusted reverse proxy strips and rewrites
		// client IP headers. Otherwise clients can spoof the rate-limit key.
		middlewares = append([]func(http.Handler) http.Handler{middleware.RealIP}, middlewares...)
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
		_, _ = io.WriteString(w, "# HELP mcproxy_uptime_seconds Process uptime in seconds.\n# TYPE mcproxy_uptime_seconds gauge\n")
		_, _ = io.WriteString(w, "mcproxy_uptime_seconds "+strconv.FormatInt(st.UptimeSeconds, 10)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_servers gauge\nmcproxy_servers "+strconv.Itoa(st.Servers)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_enabled_servers gauge\nmcproxy_enabled_servers "+strconv.Itoa(st.EnabledServers)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_policies gauge\nmcproxy_policies "+strconv.Itoa(st.Policies)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_rules gauge\nmcproxy_rules "+strconv.Itoa(st.Rules)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_counters gauge\nmcproxy_counters "+strconv.Itoa(st.Counters)+"\n")
		_, _ = io.WriteString(w, "# TYPE mcproxy_blocks_total counter\nmcproxy_blocks_total "+strconv.FormatInt(st.Blocks, 10)+"\n")
		for reason, count := range st.BlockReasons {
			_, _ = io.WriteString(w, "mcproxy_blocks_by_reason_total{reason=\""+promEscape(reason)+"\"} "+strconv.FormatInt(count, 10)+"\n")
		}
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
		api.Use(adminAuth(cfg.AdminToken))

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
			items, err := deps.Store.ListAudits(r.Context(), limit, offset, r.URL.Query().Get("event"))
			if handleStoreError(w, "list audits", err) {
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
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
				recordAdminAudit(r.Context(), deps.Store, "server.create", nil, map[string]any{"server_id": item.ID, "name": item.Name, "upstream": item.Upstream, "forwarding_mode": item.ForwardingMode})
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
					recordAdminAudit(r.Context(), deps.Store, "server.update", nil, map[string]any{"server_id": id, "name": item.Name, "upstream": item.Upstream, "forwarding_mode": item.ForwardingMode, "enabled": item.Enabled})
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
				serverID, ok := optionalQueryInt(w, r, "server_id")
				if !ok {
					return
				}
				items, err := deps.Store.ListRules(r.Context(), serverID)
				if handleStoreError(w, "list rules", err) {
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
				if err := deps.Store.DeleteRule(r.Context(), id); handleStoreError(w, "delete rule", err) {
					return
				}
				recordAdminAudit(r.Context(), deps.Store, "rule.delete", nil, map[string]any{"rule_id": id})
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

func adminAuth(token string) func(http.Handler) http.Handler {
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

			if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
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

			next.ServeHTTP(w, r)
		})
	}
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
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Token")
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
	dec := json.NewDecoder(r.Body)
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
	return limit, offset
}

func promEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return strings.ReplaceAll(value, "\n", `\n`)
}

func recordAdminAudit(ctx context.Context, st *store.Store, event string, serverID *int, details map[string]any) {
	if st == nil {
		return
	}
	if err := st.RecordAudit(ctx, "admin-api", serverID, event, details); err != nil {
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
