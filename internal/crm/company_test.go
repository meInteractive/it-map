package crm

import (
	"context"
	"testing"
)

func TestKnownCRMCompany(t *testing.T) {
	data := []byte(`{"companies":[{"name":"Employer"}],"agencies":[{"name":"Agency"}]}`)
	for _, tc := range []struct {
		name string
		ok   bool
	}{{"Employer", true}, {"Agency", true}, {"Unknown", false}} {
		found, err := isKnownCompany(data, tc.name)
		if err != nil || found != tc.ok {
			t.Fatal(tc, found, err)
		}
	}
	if _, err := isKnownCompany([]byte(`{}`), "Employer"); err == nil {
		t.Fatal("missing catalog accepted")
	}
	if _, err := (Repository{}).Company(context.Background(), "Demo", "Employer"); err == nil {
		t.Fatal("nil DB accepted")
	}
}
