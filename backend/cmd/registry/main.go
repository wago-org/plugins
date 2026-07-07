// Command registry is the HTTP backend for the wago plugins registry
// (pkg.wago.sh). It uses GitHub as the sole identity provider, issues its own
// signed-cookie sessions, and stores the full registry — packages, users, stars,
// reviews, votes, comments and install history — in a single JSON-file store
// seeded from data/packages.json on first run.
//
// It depends only on the Go standard library so it can, in principle, be built
// with TinyGo to run on the wago WASM runtime.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/wago-org/registry-backend/internal/api"
	"github.com/wago-org/registry-backend/internal/config"
	"github.com/wago-org/registry-backend/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Ensure the store's directory exists, then open (creating it if missing).
	if dir := filepath.Dir(cfg.StoreFile); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("store dir: %v", err)
		}
	}
	st, err := store.Open(cfg.StoreFile)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	// Seed from the packages file when the store is empty.
	if err := store.Seed(st, cfg.PackagesFile); err != nil {
		log.Printf("WARNING: seeding from %q failed: %v", cfg.PackagesFile, err)
	}
	log.Printf("store ready: %d packages", st.PackageCount())

	app := api.New(cfg, st)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           app.NewRouter(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("wago registry backend listening on :%s (dev=%v, frontend=%s)",
		cfg.Port, cfg.DevMode, cfg.FrontendURL)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
