package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mcproxy/internal/api"
	"mcproxy/internal/config"
	gateapp "mcproxy/internal/gate"
	"mcproxy/internal/geo"
	"mcproxy/internal/observability"
	"mcproxy/internal/store"
)

func main() {
	cfg := config.LoadFromEnv()
	if err := observability.Init(cfg.LokiHost, cfg.LogIdentify); err != nil {
		_, _ = os.Stderr.WriteString("failed to initialize logger: " + err.Error() + "\n")
		os.Exit(1)
	}
	observability.Info("mcproxy_starting", "config", cfg.String())

	st, err := store.Open(context.Background(), cfg)
	if err != nil {
		observability.Error("store_open_failed", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	var geoSvc *geo.Service
	if cfg.GeoIPPath != "" {
		g, gerr := geo.Open(cfg.GeoIPPath)
		if gerr != nil {
			observability.Warn("geo_open_failed", "error", gerr)
		} else {
			geoSvc = g
			defer geoSvc.Close()
		}
	}

	router := api.NewRouter(cfg, api.Dependencies{Store: st, Geo: geoSvc})

	gateRuntime, err := gateapp.New(context.Background(), gateapp.Options{
		Bind:       cfg.GateBind,
		OnlineMode: cfg.GateOnlineMode,
		Store:      st,
	})
	if err != nil {
		observability.Warn("gate_init_skipped", "error", err)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		observability.Error("http_listen_failed", "error", err)
		os.Exit(1)
	}

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	if gateRuntime != nil {
		go func() {
			if err := gateRuntime.Start(ctx); err != nil {
				errCh <- err
			}
		}()
	}

	observability.Info("http_api_listening", "addr", cfg.HTTPAddr)
	if gateRuntime != nil {
		observability.Info("gate_listening", "addr", gateRuntime.Bind())
	}

	select {
	case err := <-errCh:
		observability.Error("server_runtime_error", "error", err)
		os.Exit(1)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		observability.Error("http_server_shutdown_error", "error", err)
	}
}
