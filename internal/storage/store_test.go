package storage

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestDSNValidationAndRedaction(t *testing.T) {
	base := Settings{Host: "127.0.0.1", Port: "3306", Database: "itmap_staging", User: "itmap_app"}
	secret := "not-for-logs:pass@word"
	dsn, err := MakeDSN(base, secret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, secret+"@tcp(") || !strings.Contains(dsn, "parseTime=true") || !strings.Contains(dsn, "multiStatements=false") || !strings.Contains(dsn, "clientFoundRows=true") {
		t.Fatalf("bad DSN parameters (password redacted)")
	}
	cases := []Settings{
		{Host: "external.example", Port: "3306", Database: "staging", User: "a"},
		{Host: "localhost", Port: "99999", Database: "staging", User: "a"},
		{Host: "localhost", Port: "3306", Database: "name;DROP TABLE x", User: "a"},
		{Host: "localhost", Port: "3306", Database: "staging", User: "bad@name"},
	}
	for _, s := range cases {
		if _, err := MakeDSN(s, secret); err == nil {
			t.Fatal("invalid database settings were accepted")
		}
	}
	if _, err := MakeDSN(base, ""); err == nil {
		t.Fatal("empty password permitted")
	}
}

func TestBaselineMetadata(t *testing.T) {
	if !strings.Contains(string(baselineJSON), "\"company_vacancies\"") {
		t.Fatal("baseline missing vacancy table")
	}
	if !strings.Contains(string(baselineJSON), "_d1_active_scan") {
		t.Fatal("baseline missing generated DB column")
	}
}

func TestWithinTxRejectsMissingInputs(t *testing.T) {
	err := WithinTx(context.Background(), nil, nil, func(*sql.Tx) error { return nil })
	if err == nil {
		t.Fatal("nil DB must be rejected")
	}
}

func TestCompareBaseline(t *testing.T) {
	expected := map[string]map[string]string{"vacancies": {"id": "bigint", "title": "longtext"}}
	for _, tc := range []struct {
		name      string
		actual    map[string]map[string]string
		wantError bool
	}{
		{"same", map[string]map[string]string{"vacancies": {"id": "bigint", "title": "longtext"}}, false},
		{"extra column", map[string]map[string]string{"vacancies": {"id": "bigint", "title": "longtext", "new_column": "int"}}, false},
		{"missing table", map[string]map[string]string{}, true},
		{"missing field", map[string]map[string]string{"vacancies": {"id": "bigint"}}, true},
		{"changed field type", map[string]map[string]string{"vacancies": {"id": "int", "title": "longtext"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := compareBaseline(expected, tc.actual)
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v, wanted error=%v", err, tc.wantError)
			}
		})
	}
}
