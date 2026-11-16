package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAuditRequiresOptInAndRedactsFailures(t *testing.T) {
	env := map[string]string{"ITMAP_SCHEMA_AUDIT": "1", "DB_HOST": "127.0.0.1", "DB_PORT": "3306", "DB_NAME": "ci_db", "DB_USER": "ci_user", "DB_PASSWORD": "ci_private_password"}
	getter := func(k string) string { return env[k] }
	called := false
	audit := func(context.Context, string) error { called = true; return errors.New("ci_private_password") }
	var out bytes.Buffer
	if run(context.Background(), getter, audit, &out) != 1 || !called || strings.Contains(out.String(), "ci_private_password") || !strings.Contains(out.String(), "schema_audit_unavailable") {
		t.Fatal("failure not safely reported", out.String())
	}
	called = false
	out.Reset()
	env["ITMAP_SCHEMA_AUDIT"] = "0"
	if run(context.Background(), getter, audit, &out) != 2 || called {
		t.Fatal("audit not gated")
	}
	env["ITMAP_SCHEMA_AUDIT"] = "1"
	env["DB_HOST"] = "remote.invalid"
	out.Reset()
	if run(context.Background(), getter, audit, &out) != 2 || called {
		t.Fatal("remote DB accepted")
	}
}

func TestAuditResultIsFixed(t *testing.T) {
	env := map[string]string{"ITMAP_SCHEMA_AUDIT": "1", "DB_HOST": "127.0.0.1", "DB_PORT": "3306", "DB_NAME": "ci_db", "DB_USER": "ci_user", "DB_PASSWORD": "ci_password"}
	getter := func(k string) string { return env[k] }
	for _, tc := range []struct {
		err  error
		code int
		want string
	}{{nil, 0, "schema_audit_compatible\n"}, {errIncompatible, 1, "schema_audit_mismatch_or_incomplete\n"}, {errUnavailable, 1, "schema_audit_unavailable\n"}} {
		var out bytes.Buffer
		code := run(context.Background(), getter, func(context.Context, string) error { return tc.err }, &out)
		if code != tc.code || out.String() != tc.want {
			t.Fatal("unexpected result", code, out.String())
		}
	}
}
