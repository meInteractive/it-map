// Package config loads only non-secret configuration into the initial Go shell.
// API keys and DB passwords must be supplied at runtime when the respective
// integrations are implemented, and must never be logged.
package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
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
	DBEnabled   bool
	AuthEnabled bool
}

// enabledEnv is strict: an accidental AUTH_ENABLED=tru must not silently
// disable the staging authentication gate.  Keep the environment contract
// explicit and easy to audit in deployment configuration.
func enabledEnv(key string) (bool, error) {
	raw := getenv(key, "false")
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", key)
	}
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
	dbEnabled, err := enabledEnv("DB_ENABLED")
	if err != nil {
		return Config{}, err
	}
	authEnabled, err := enabledEnv("AUTH_ENABLED")
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Environment: getenv("APP_ENV", "development"),
		HTTPAddr:    getenv("HTTP_ADDR", "127.0.0.1:8080"),
		DBHost:      getenv("DB_HOST", "127.0.0.1"),
		DBPort:      getenv("DB_PORT", "3306"),
		DBName:      getenv("DB_NAME", ""),
		DBUser:      getenv("DB_USER", ""),
		DBEnabled:   dbEnabled,
		AuthEnabled: authEnabled,
	}
	return cfg, cfg.Validate()
}

// Validate is kept separate from Load so tests do not mutate global env state.
func (cfg Config) Validate() error {
	if e := ValidateEnvironment(cfg.Environment); e != nil {
		return e
	}
	if cfg.Environment == "staging" && (!cfg.DBEnabled || !cfg.AuthEnabled) {
		return errors.New("staging requires DB_ENABLED=true and AUTH_ENABLED=true")
	}

	host, port, err := net.SplitHostPort(cfg.HTTPAddr)
	if err != nil || host == "" || port == "" {
		return errors.New("HTTP_ADDR must be a host:port address")
	}
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		return errors.New("HTTP_ADDR must bind to loopback; publish only through a reverse proxy")
	}
	if cfg.AuthEnabled {
		if len(os.Getenv("SESSION_SECRET")) < 32 || os.Getenv("SITE_PASSWORD") == "" {
			return errors.New("AUTH_ENABLED requires SESSION_SECRET (32+ bytes) and SITE_PASSWORD")
		}
	}
	if cfg.DBEnabled {
		if cfg.DBName == "" || cfg.DBUser == "" {
			return errors.New("DB_NAME and DB_USER required when DB_ENABLED=true")
		}
		port, err := strconv.Atoi(cfg.DBPort)
		if err != nil || port <= 0 || port > 65535 {
			return errors.New("invalid DB_PORT")
		}
	}
	if cfg.DBHost != "127.0.0.1" && cfg.DBHost != "localhost" {
		return errors.New("DB_HOST must be localhost for this deployment")
	}
	return nil
}

// ValidateEnvironment preserves the migration's production block for explicit
// administrative commands without requiring HTTP/auth credentials for them.
func ValidateEnvironment(environment string) error {
	switch environment {
	case "development", "test", "staging":
		return nil
	case "production":
		return errors.New("production is explicitly disabled until sprint 1.1 has passed acceptance")
	default:
		return fmt.Errorf("invalid APP_ENV value %q", environment)
	}
}
