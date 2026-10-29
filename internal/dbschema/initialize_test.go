package dbschema

import (
	"context"
	"errors"
	"testing"
)

func TestInitServerVersionBoundary(t *testing.T) {
	for _, v := range []string{"10.11.14-MariaDB-ubu2204", "11.4.8-MariaDB", "5.5.5-10.11.14-MariaDB"} {
		if !supportedVersion(v) || CheckServerVersion(v) != nil {
			t.Fatal("supported MariaDB version refused")
		}
	}
	for _, v := range []string{"10.3.32-MariaDB", "5.5.5-10.3.32-MariaDB", "10.3.39-MariaDB", "10.10.9-MariaDB", "8.0.40", "10.11.1-MySQL", "unrecognized", "9999999999999999999999.11.1-MariaDB", "10.9999999999999999999999.1-MariaDB"} {
		if supportedVersion(v) || !errors.Is(CheckServerVersion(v), ErrUnsupportedServer) {
			t.Fatal("unsupported server accepted")
		}
	}
	if _, e := Initialize(context.Background(), nil); e == nil {
		t.Fatal("nil storage initialized")
	}
}
