package api

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
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
}

func NewRouter(cfg config.Config, deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// Basic hardening middlewares
	r.Use(
		middleware.RequestID,
		middleware.RealIP,
		middleware.Recoverer,
		middleware.Timeout(60*time.Second),
		observability.RequestLogger,
		securityHeaders,
	)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"time":   time.Now().UTC().Format(time.RFC3339Nano),
		})
	})

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(middleware.ThrottleBacklog(cfg.AdminThrottle, cfg.AdminThrottle*2, 30*time.Second))
		api.Use(limitBody(1 << 20))
		api.Use(adminAuth(cfg.AdminToken))

		api.Get("/stats", func(w http.ResponseWriter, r *http.Request) {
			st, err := deps.Store.Stats(r.Context())
			if err != nil {
				// Avoid leaking internal errors to clients
				log.Printf("stats handler error: %v", err)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
				return
			}
			writeJSON(w, http.StatusOK, st)
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
				writeJSON(w, http.StatusOK, item)
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
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				observability.LogJSON("warn", "admin_api_disabled", map[string]any{"path": r.URL.Path})
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "admin api disabled: MCPROXY_ADMIN_TOKEN is not configured"})
				return
			}

			provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
			if provided == "" {
				provided = strings.TrimSpace(r.Header.Get("X-API-Token"))
			}

			if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
				observability.LogJSON("warn", "admin_auth_failed", map[string]any{"path": r.URL.Path, "remote_ip": r.RemoteAddr})
				writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
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
		log.Printf("writeJSON encode error: %v", err)
	}
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
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
	log.Printf("%s: %v", action, err)
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
}
