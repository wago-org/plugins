// Command registry is the HTTP backend for the wago plugins registry
// (plugins.wago.sh). It uses GitHub as the sole identity provider, issues its own
// signed-cookie sessions, and stores the full registry — packages, users, stars,
// reviews, votes, comments and install history. The default store engine is an
// embedded Pebble (RocksDB-lineage LSM) database seeded from data/packages.json
// on first run; set STORE_ENGINE=json for the legacy single-file store.
//
// The API layer keeps the dataset in memory and serves the hot anonymous browse
// paths (package list + detail) from a short-TTL response cache, so read
// throughput is decoupled from the per-request work and the write rate.
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

	// Open the configured store engine. Pebble (embedded LSM) is the default;
	// "json" selects the legacy single-file store.
	var st store.Store
	switch cfg.StoreEngine {
	case "json":
		if dir := filepath.Dir(cfg.StoreFile); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				log.Fatalf("store dir: %v", err)
			}
		}
		js, err := store.Open(cfg.StoreFile)
		if err != nil {
			log.Fatalf("open store: %v", err)
		}
		st = js
	default: // "pebble"
		if err := os.MkdirAll(cfg.StoreDir, 0o755); err != nil {
			log.Fatalf("store dir: %v", err)
		}
		ps, err := store.OpenPebble(cfg.StoreDir)
		if err != nil {
			log.Fatalf("open store: %v", err)
		}
		defer ps.Close()
		st = ps
	}
	if reporter, ok := st.(store.MigrationReporter); ok {
		if migration, migrated := reporter.StartupMigration(); migrated {
			log.Printf("store migrated: schema %d -> %d, canonical packages=%d, quarantined v0 packages=%d",
				migration.FromVersion, migration.ToVersion, migration.CanonicalPackages, migration.QuarantinedPackages)
		}
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
