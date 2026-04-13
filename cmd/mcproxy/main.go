package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"mcproxy/internal/api"
	"mcproxy/internal/config"
	"mcproxy/internal/geo"
	"mcproxy/internal/store"
)

func main() {
	cfg := config.LoadFromEnv()

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

	srv := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}

	log.Printf("mcproxy http api listening on %s", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("http server error: %v", err)
		os.Exit(1)
	}
}
