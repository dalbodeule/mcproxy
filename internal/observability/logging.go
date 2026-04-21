package observability

import (
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/grafana/loki-client-go/loki"
	slogloki "github.com/samber/slog-loki/v3"
	slogmulti "github.com/samber/slog-multi"
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

	config, _ := loki.NewDefaultConfig(lokiHost)
	config.TenantID = "mcproxy"
	client, err := loki.New(config)
	if err != nil {
		slog.Error("Failed to create Loki client", "error", err)
		return nil, err
	}

	logger := slog.New(
		slogmulti.Fanout(
			slog.NewTextHandler(os.Stdout, nil),
			slogloki.Option{Level: slog.LevelDebug, Client: client}.NewLokiHandler(),
		),
	)
	logger = logger.With(
		slog.String("app", "mcproxy"),
		slog.String("identify", identify),
	)
	logger.Info("Logging to Loki", "host", lokiHost)

	return logger, nil
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
