// Package config loads application configuration from environment variables
// (and an optional .env file) into a typed Config struct.
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration for the API.
type Config struct {
	// AppEnv is the deployment environment: "development", "staging" or "production".
	AppEnv string
	// Port is the TCP port the HTTP server listens on (without the colon).
	Port string
	// DatabaseURL is the full Postgres connection string (pgx/libpq format).
	DatabaseURL string
	// LogLevel controls zerolog verbosity: trace|debug|info|warn|error.
	LogLevel string
	// JWTSecret signs and verifies access tokens (HMAC). Required.
	JWTSecret string
	// AIEncryptionKey (32 bytes) encrypts user-supplied AI API keys at rest
	// (AES-256-GCM). Optional: when empty, AI features return 503 and the rest of
	// the app runs normally. Set AI_ENCRYPTION_KEY to 64 hex chars.
	AIEncryptionKey []byte
}

// IsProduction reports whether the app runs in a production-like environment.
func (c Config) IsProduction() bool {
	return c.AppEnv == "production"
}

// Load reads configuration from the environment. It first attempts to load a
// .env file (silently ignored if absent, so real env vars work in prod) and
// then maps each value into Config, applying defaults where sensible.
//
// DatabaseURL is required and Load returns an error if it is missing, since the
// app cannot do anything useful without a database.
func Load() (Config, error) {
	// .env is optional: in production we rely on real environment variables.
	_ = godotenv.Load()

	cfg := Config{
		AppEnv:      getEnv("APP_ENV", "development"),
		Port:        getEnv("APP_PORT", "8080"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		LogLevel:    getEnv("LOG_LEVEL", "debug"),
		JWTSecret:   getEnv("JWT_SECRET", ""),
	}

	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}
	if strings.TrimSpace(cfg.JWTSecret) == "" {
		return Config{}, fmt.Errorf("config: JWT_SECRET is required")
	}

	// AI_ENCRYPTION_KEY is optional; if present it must decode to exactly 32 bytes.
	if raw := getEnv("AI_ENCRYPTION_KEY", ""); raw != "" {
		key, err := hex.DecodeString(raw)
		if err != nil || len(key) != 32 {
			return Config{}, fmt.Errorf("config: AI_ENCRYPTION_KEY must be 64 hex chars (32 bytes)")
		}
		cfg.AIEncryptionKey = key
	}

	return cfg, nil
}

// getEnv returns the value of the env var key, or fallback if it is unset/empty.
// Values are trimmed so stray whitespace (e.g. from Makefile-exported .env vars
// with inline comments) can't corrupt things like the listen port.
func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return fallback
}
