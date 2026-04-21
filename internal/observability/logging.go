package observability

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func LogJSON(level, msg string, fields map[string]any) {
	entry := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339Nano),
		"level": level,
		"msg":   msg,
	}
	for k, v := range fields {
		entry[k] = v
	}
	b, err := json.Marshal(entry)
	if err != nil {
		log.Printf("{\"level\":\"error\",\"msg\":\"log marshal failed\",\"err\":%q}", err.Error())
		return
	}
	log.Print(string(b))
}

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		LogJSON("info", "http_request", map[string]any{
			"request_id":  middleware.GetReqID(r.Context()),
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      ww.Status(),
			"bytes":       ww.BytesWritten(),
			"duration_ms": time.Since(start).Milliseconds(),
			"remote_ip":   r.RemoteAddr,
		})
	})
}
