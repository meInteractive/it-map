package storage

import (
	"errors"
	"regexp"
	"testing"
)

var disposableUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Target selection is intentionally stricter than DB name alone. This is a
// test-only guard: it must never become a general-purpose database connector.
func requireDisposableTarget(network, address, database, user string) error {
	if network != "tcp" || address != "127.0.0.1:3306" || database != "itmap_ci" || user != "itmap_ci" {
		return errors.New("refusing non-disposable MariaDB write target")
	}
	return nil
}

func TestRequireDisposableTarget(t *testing.T) {
	for _, tc := range []struct {
		name, network, address, db, user string
		valid                            bool
	}{
		{"local CI", "tcp", "127.0.0.1:3306", "itmap_ci", "itmap_ci", true},
		{"remote host", "tcp", "192.0.2.1:3306", "itmap_ci", "itmap_ci", false},
		{"different port", "tcp", "127.0.0.1:3307", "itmap_ci", "itmap_ci", false},
		{"different schema", "tcp", "127.0.0.1:3306", "itmap_prod", "itmap_ci", false},
		{"wrong user", "tcp", "127.0.0.1:3306", "itmap_ci", "root", false},
		{"unix socket", "unix", "/tmp/mysql.sock", "itmap_ci", "itmap_ci", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := requireDisposableTarget(tc.network, tc.address, tc.db, tc.user)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected target validation result: accepted=%t", err == nil)
			}
		})
	}
}

// The marker exists solely to prevent CI write tests from targeting a reused,
// shared or similarly named database. Never put its value in a test failure.
func requireDisposableGeneration(expected, actual string) error {
	if !disposableUUID.MatchString(expected) || actual != expected {
		return errors.New("CI disposable database marker missing or mismatched")
	}
	return nil
}

func TestRequireDisposableGeneration(t *testing.T) {
	const marker = "d2ba75a1-07fa-4895-b9b1-2cf16ae2f880"
	for _, tc := range []struct {
		name, expected, actual string
		valid                  bool
	}{
		{"same run", marker, marker, true},
		{"different run", marker, "6a276ee0-3455-4381-b8a7-0d0ac35c3837", false},
		{"empty expected", "", marker, false},
		{"empty actual", marker, "", false},
		{"not a UUID", "static_ci_marker", "static_ci_marker", false},
		{"unexpected case", "D2BA75A1-07FA-4895-B9B1-2CF16AE2F880", "D2BA75A1-07FA-4895-B9B1-2CF16AE2F880", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := requireDisposableGeneration(tc.expected, tc.actual)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected marker validation result: accepted=%t", err == nil)
			}
		})
	}
}
