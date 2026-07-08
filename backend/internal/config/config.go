// Package config loads the registry backend's runtime configuration from the
// environment. It has no dependencies on the other internal packages so it sits
// at the bottom of the import graph.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"os"
	"strings"
)

// Config is the fully-resolved runtime configuration.
type Config struct {
	Port               string
	GithubClientID     string
	GithubClientSecret string
	OAuthRedirectURL   string
	FrontendURL        string
	SessionSecret      []byte
	PackagesFile       string
	StoreEngine        string // "pebble" (default, embedded LSM) or "json" (legacy file)
	StoreFile          string // json engine: path to the single JSON document
	StoreDir           string // pebble engine: path to the database directory
	DevMode            bool
	CookieDomain       string

	// SMTP for outbound mail (secondary-email verification). When SMTPHost is
	// empty, mail is logged instead of sent (dev fallback).
	SMTPHost string
	SMTPPort string
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	// AdminLogins are GitHub logins granted site-wide admin (moderate any
	// package's discussion, etc.). Set via ADMIN_LOGINS (comma-separated).
	AdminLogins []string
}

// IsAdmin reports whether the given GitHub login has site-wide admin rights.
func (c Config) IsAdmin(login string) bool {
	for _, a := range c.AdminLogins {
		if strings.EqualFold(a, login) {
			return true
		}
	}
	return false
}

// getenv returns the env var, or def when unset/empty.
func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// splitList parses a comma-separated env value into a trimmed, non-empty slice.
func splitList(v string) []string {
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load builds a Config from environment variables, applying defaults and
// emitting warnings for anything that would break OAuth or session persistence.
func Load() (Config, error) {
	c := Config{
		Port:               getenv("PORT", "8787"),
		GithubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GithubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		OAuthRedirectURL:   os.Getenv("OAUTH_REDIRECT_URL"),
		FrontendURL:        getenv("FRONTEND_URL", "http://localhost:8000"),
		PackagesFile:       getenv("PACKAGES_FILE", "./data/packages.json"),
		StoreEngine:        getenv("STORE_ENGINE", "pebble"),
		StoreFile:          getenv("STORE_FILE", "./data/store.json"),
		StoreDir:           getenv("STORE_DIR", "./data/registry-db"),
		DevMode:            os.Getenv("DEV_MODE") == "true",
		CookieDomain:       os.Getenv("COOKIE_DOMAIN"),
		SMTPHost:           os.Getenv("SMTP_HOST"),
		SMTPPort:           getenv("SMTP_PORT", "587"),
		SMTPUser:           os.Getenv("SMTP_USER"),
		SMTPPass:           os.Getenv("SMTP_PASS"),
		SMTPFrom:           os.Getenv("SMTP_FROM"),
		AdminLogins:        splitList(os.Getenv("ADMIN_LOGINS")),
	}

	if secret := os.Getenv("SESSION_SECRET"); secret != "" {
		c.SessionSecret = []byte(secret)
	} else {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return Config{}, err
		}
		c.SessionSecret = []byte(base64.RawURLEncoding.EncodeToString(b))
		log.Printf("WARNING: SESSION_SECRET is empty; generated an ephemeral key. " +
			"Sessions will NOT survive a restart. Set SESSION_SECRET in production.")
	}

	if c.GithubClientID == "" || c.GithubClientSecret == "" {
		log.Printf("WARNING: GITHUB_CLIENT_ID / GITHUB_CLIENT_SECRET are not set; " +
			"GitHub login will not work until configured.")
	}
	if c.OAuthRedirectURL == "" {
		log.Printf("WARNING: OAUTH_REDIRECT_URL is not set; GitHub login will not work.")
	}

	return c, nil
}
