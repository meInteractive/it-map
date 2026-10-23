// Package config loads only non-secret configuration into the initial Go shell.
// API keys and DB passwords must be supplied at runtime when the respective
// integrations are implemented, and must never be logged.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// Config contains runtime settings needed to run the not-yet-migrated HTTP shell.
type Config struct {
	Environment string
	HTTPAddr    string
	DBHost      string
	DBPort      string
	DBName      string
	DBUser      string
}

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

// Load reads environment variables and ensures that the migration shell cannot
// be accidentally promoted to production before the business API and DB are ready.
func Load() (Config, error) {
	cfg := Config{
		Environment: getenv("APP_ENV", "development"),
		HTTPAddr:    getenv("HTTP_ADDR", "127.0.0.1:8080"),
		DBHost:      getenv("DB_HOST", "127.0.0.1"),
		DBPort:      getenv("DB_PORT", "3306"),
		DBName:      getenv("DB_NAME", ""),
		DBUser:      getenv("DB_USER", ""),
	}
	return cfg, cfg.Validate()
}

// Validate is kept separate from Load so tests do not mutate global env state.
func (cfg Config) Validate() error {
	switch cfg.Environment {
	case "development", "test", "staging":
	case "production":
		return errors.New("production is explicitly disabled until sprint 1.1 has passed acceptance")
	default:
		return fmt.Errorf("invalid APP_ENV value %q", cfg.Environment)
	}
	host, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil || host == "" || port == "" {
		return errors.New("HTTP_ADDR must be a host:port address")
	}
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		return errors.New("HTTP_ADDR must bind to loopback; publish only through a reverse proxy")
	}
	if cfg.DBHost != "127.0.0.1" && cfg.DBHost != "localhost" {
		return errors.New("DB_HOST must be localhost for this deployment")
	}
	return nil
}
