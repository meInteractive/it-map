package main

import (
	"strings"
	"testing"
)

func TestRequiresIsolatedStagingConfiguration(t *testing.T) {
	valid := settings{environment: "staging", enabled: "1", host: "127.0.0.1", port: "3306", database: "itmap_parser_staging", user: "itmap_parser_app", password: "synthetic", secret: strings.Repeat("x", 32), sitePassword: "synthetic"}
	if err := valid.validate(); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*settings){
		"environment":     func(s *settings) { s.environment = "production" },
		"explicit opt-in": func(s *settings) { s.enabled = "" },
		"host":            func(s *settings) { s.host = "localhost" },
		"port":            func(s *settings) { s.port = "3307" },
		"database":        func(s *settings) { s.database = "itmap_main" },
		"user":            func(s *settings) { s.user = "root" },
		"password":        func(s *settings) { s.password = "" },
		"secret":          func(s *settings) { s.secret = "short" },
		"site password":   func(s *settings) { s.sitePassword = "" },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			copy := valid
			change(&copy)
			if copy.validate() == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
}
