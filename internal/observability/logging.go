package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

var (
	defaultLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	mu            sync.RWMutex
)

func getLogger(lokiHost string, identify string) (*slog.Logger, error) {
	if lokiHost == "" {
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		logger.Info("Loki host is not set. Logging to stdout")
		return logger.With(
			slog.String("app", "mcproxy"),
			slog.String("identify", identify),
		), nil
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger = slog.New(&lokiHandler{next: logger.Handler(), endpoint: strings.TrimRight(lokiHost, "/") + "/loki/api/v1/push", client: &http.Client{Timeout: 2 * time.Second}})
	logger = logger.With(
		slog.String("app", "mcproxy"),
		slog.String("identify", identify),
	)
	logger.Info("Logging to Loki", "host", lokiHost)

	return logger, nil
}

type lokiHandler struct {
	next     slog.Handler
	endpoint string
	client   *http.Client
	attrs    []slog.Attr
}

type lokiPush struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

func (h *lokiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= slog.LevelDebug || h.next.Enabled(ctx, level)
}

func (h *lokiHandler) Handle(ctx context.Context, record slog.Record) error {
	if err := h.next.Handle(ctx, record); err != nil {
		return err
	}
	labels := make(map[string]string, len(h.attrs)+4)
	for _, attr := range h.attrs {
		labels[sanitizeLabel(attr.Key)] = attr.Value.String()
	}
	record.Attrs(func(attr slog.Attr) bool {
		labels[sanitizeLabel(attr.Key)] = attr.Value.String()
		return true
	})
	if len(labels) == 0 {
		labels["app"] = "mcproxy"
	}
	payload := lokiPush{Streams: []lokiStream{{
		Stream: labels,
		Values: [][2]string{{timeToNanoseconds(record.Time), record.Message}},
	}}}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scope-OrgID", "mcproxy")
	resp, err := h.client.Do(req)
	if err != nil {
		return nil // Loki is an optional sink; stdout remains authoritative.
	}
	_ = resp.Body.Close()
	return nil
}

func (h *lokiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := *h
	cloned.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	cloned.next = h.next.WithAttrs(attrs)
	return &cloned
}

func (h *lokiHandler) WithGroup(name string) slog.Handler {
	cloned := *h
	cloned.next = h.next.WithGroup(name)
	return &cloned
}

func sanitizeLabel(value string) string {
	var b strings.Builder
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9' && i > 0) || r == '_' || r == ':' {
			b.WriteRune(r)
		} else if i == 0 {
			b.WriteString("_")
		}
	}
	if b.Len() == 0 {
		return "label"
	}
	return b.String()
}

func timeToNanoseconds(value time.Time) string {
	return strconv.FormatInt(value.UnixNano(), 10)
}

func Init(lokiHost string, identify string) error {
	logger, err := getLogger(lokiHost, identify)
	if err != nil {
		return err
	}
	SetDefault(logger)
	return nil
}

func SetDefault(logger *slog.Logger) {
	mu.Lock()
	defer mu.Unlock()
	defaultLogger = logger
	slog.SetDefault(logger)
}

func L() *slog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return defaultLogger
}

func With(args ...any) *slog.Logger { return L().With(args...) }
func Info(msg string, args ...any)  { L().Info(msg, args...) }
func Warn(msg string, args ...any)  { L().Warn(msg, args...) }
func Error(msg string, args ...any) { L().Error(msg, args...) }

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		L().InfoContext(r.Context(), "http_request",
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", ww.Status()),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("remote_ip", r.RemoteAddr),
		)
	})
}
