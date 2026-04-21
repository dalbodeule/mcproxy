package main

import (
	"context"
	"log"
	"net"
	"net/http"
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
	observability.LogJSON("info", "mcproxy_starting", map[string]any{"config": cfg.String()})

	// Store (DB)
	st, err := store.Open(context.Background(), cfg)
	if err != nil {
		log.Fatalf("store open: %v", err)
	}
	defer st.Close()

	// GeoIP (optional until path provided)
	var geoSvc *geo.Service
	if cfg.GeoIPPath != "" {
		g, gerr := geo.Open(cfg.GeoIPPath)
		if gerr != nil {
			log.Printf("geo open failed (continuing without GeoIP): %v", gerr)
		} else {
			geoSvc = g
			defer geoSvc.Close()
		}
	}

	// HTTP API
	router := api.NewRouter(cfg, api.Dependencies{Store: st, Geo: geoSvc})

	gateRuntime, err := gateapp.New(context.Background(), gateapp.Options{
		Bind:       cfg.GateBind,
		OnlineMode: cfg.GateOnlineMode,
		Store:      st,
	})
	if err != nil {
		log.Printf("gate init skipped: %v", err)
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		log.Fatalf("http listen: %v", err)
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

	log.Printf("mcproxy http api listening on %s", cfg.HTTPAddr)
	if gateRuntime != nil {
		log.Printf("mcproxy gate listening on %s", gateRuntime.Bind())
	}

	select {
	case err := <-errCh:
		log.Fatalf("http server error: %v", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown error: %v", err)
	}
}
