package config

import (
	"strings"
	"testing"
)

func TestConfigValidity(t *testing.T) {
	t.Setenv("SESSION_SECRET", strings.Repeat("x", 40))
	t.Setenv("SITE_PASSWORD", "synthetic-password")
	ok := Config{Environment: "staging", HTTPAddr: "127.0.0.1:8080", DBHost: "127.0.0.1", DBPort: "3306", DBName: "itmap_ci", DBUser: "itmap_ci", DBEnabled: true, AuthEnabled: true}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []Config{
		{Environment: "production", HTTPAddr: "127.0.0.1:8080", DBHost: "127.0.0.1"},
		{Environment: "staging", HTTPAddr: "0.0.0.0:8080", DBHost: "127.0.0.1"},
		{Environment: "staging", HTTPAddr: "127.0.0.1:8080", DBHost: "db.example.org"},
		{Environment: "unknown", HTTPAddr: "127.0.0.1:8080", DBHost: "127.0.0.1"},
	} {
		if err := tc.Validate(); err == nil {
			t.Errorf("wanted failure for environment %q and addr %q", tc.Environment, tc.HTTPAddr)
		}
	}
}
func TestLoadDoesNotExposeSecrets(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("HTTP_ADDR", "127.0.0.1:8090")
	t.Setenv("SESSION_SECRET", "supersecret-value-that-must-not-be-logged")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cfg.HTTPAddr, "supersecret") {
		t.Fatal("secret leaked to config")
	}
}

func TestStagingRejectsDisabledControls(t *testing.T) {
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 40))
	t.Setenv("SITE_PASSWORD", "synthetic-password")
	ok := Config{Environment: "staging", HTTPAddr: "127.0.0.1:8080", DBHost: "127.0.0.1", DBPort: "3306", DBName: "itmap_ci", DBUser: "itmap_ci", DBEnabled: true, AuthEnabled: true}
	for _, tc := range []struct{ db, auth bool }{
		{false, true}, {true, false}, {false, false},
	} {
		cfg := ok
		cfg.DBEnabled = tc.db
		cfg.AuthEnabled = tc.auth
		if err := cfg.Validate(); err == nil {
			t.Fatalf("staging accepted DB_ENABLED=%v AUTH_ENABLED=%v", tc.db, tc.auth)
		}
	}
	dev := ok
	dev.Environment = "development"
	dev.DBEnabled, dev.AuthEnabled = false, false
	if err := dev.Validate(); err != nil {
		t.Fatalf("development without external services should remain supported: %v", err)
	}
}

func TestEnabledEnvRejectsMisspelling(t *testing.T) {
	for _, key := range []string{"DB_ENABLED", "AUTH_ENABLED"} {
		t.Setenv(key, "treu")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("expected %s typo to fail explicitly: %v", key, err)
		}
		t.Setenv(key, "false")
	}
}

func TestLoadStagingRequiresBothServices(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("HTTP_ADDR", "127.0.0.1:18080")
	t.Setenv("DB_HOST", "127.0.0.1")
	t.Setenv("DB_PORT", "3306")
	t.Setenv("DB_NAME", "itmap_ci")
	t.Setenv("DB_USER", "itmap_ci")
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 40))
	t.Setenv("SITE_PASSWORD", "synthetic-password")
	for _, tc := range []struct {
		db, auth string
		valid    bool
	}{
		{"true", "true", true},
		{"false", "true", false},
		{"true", "false", false},
		{"false", "false", false},
		{"treu", "true", false},
	} {
		t.Setenv("DB_ENABLED", tc.db)
		t.Setenv("AUTH_ENABLED", tc.auth)
		cfg, err := Load()
		if (err == nil) != tc.valid {
			t.Errorf("staging config db=%q auth=%q accepted=%t: %v", tc.db, tc.auth, err == nil, err)
		}
		if err == nil && (!cfg.DBEnabled || !cfg.AuthEnabled || cfg.Environment != "staging") {
			t.Errorf("staging loaded with incomplete settings: %+v", cfg)
		}
	}
}

func TestAuthRequiresSecretsAtStartup(t *testing.T) {
	t.Setenv("SESSION_SECRET", "")
	t.Setenv("SITE_PASSWORD", "")
	cfg := Config{Environment: "development", HTTPAddr: "127.0.0.1:8080", DBHost: "127.0.0.1", AuthEnabled: true}
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled auth without secrets accepted")
	}
	t.Setenv("SESSION_SECRET", strings.Repeat("s", 40))
	t.Setenv("SITE_PASSWORD", "correct")
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}
