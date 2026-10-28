package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestSchemaCommandValidationBeforeConnection(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	const password = "CI synthetic password never logged"
	for _, test := range []struct {
		name     string
		args     []string
		password string
		want     int
	}{
		{"help", []string{"--help"}, password, 0},
		{"missing database", []string{"--user", "itmap_ci"}, password, 2},
		{"remote host", []string{"--host", "192.0.2.1", "--database", "itmap_ci", "--user", "itmap_ci"}, password, 2},
		{"missing password", []string{"--database", "itmap_ci", "--user", "itmap_ci"}, "", 2},
		{"positional SQL refused", []string{"CREATE TABLE"}, password, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, err bytes.Buffer
			code := schemaInitCommand(context.Background(), test.args, test.password, &out, &err)
			if code != test.want {
				t.Fatal("schema command decision differs", code)
			}
			if strings.Contains(out.String()+err.String(), password) {
				t.Fatal("schema command exposed password")
			}
			if test.want == 0 && !strings.Contains(err.String(), "offline") {
				t.Fatal("help did not describe offline use")
			}
		})
	}
}

func TestSchemaCommandPreservesProductionBlock(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	var out, diagnostics bytes.Buffer
	if code := schemaInitCommand(context.Background(), []string{"--database", "itmap_ci", "--user", "itmap_ci"}, "CI synthetic password", &out, &diagnostics); code != 2 || !strings.Contains(diagnostics.String(), "production is explicitly disabled") {
		t.Fatal("schema command bypassed production block")
	}
}
