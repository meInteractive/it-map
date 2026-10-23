package config

import (
	"strings"
	"testing"
)

func TestConfigValidity(t *testing.T) {
	ok := Config{Environment: "staging", HTTPAddr: "127.0.0.1:8080", DBHost: "127.0.0.1"}
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
