package apifysettings

import (
	"errors"
	"testing"
)

func TestHHSourceUsesOnlyOwnersCatalogURL(t *testing.T) {
	catalog := []byte(`{"companies":[{"name":"CI Employer","vacancySources":{"hh":"https://hh.ru/employer/123"}},{"name":"CI Other","vacancySources":{"hh":"https://hh.ru/employer/456"}}],"agencies":[]}`)
	url, err := catalogHHSource(catalog, "CI Employer")
	if err != nil || url != "https://hh.ru/employer/123" {
		t.Fatalf("wrong owner URL: %q %v", url, err)
	}
	if _, err = catalogHHSource(catalog, "CI Missing"); !errors.Is(err, ErrSourceMissing) {
		t.Fatal("unknown owner accepted", err)
	}
	if err = validateHHSource("https://hh.ru/employer/123", "https://hh.ru/employer/456", true); !errors.Is(err, ErrChanged) {
		t.Fatal("stale or foreign URL accepted", err)
	}
	if err = validateHHSource("https://evil.invalid/123", "https://evil.invalid/123", true); !errors.Is(err, ErrInput) {
		t.Fatal("non-HH URL accepted", err)
	}
	if err = validateHHSource("https://hh.ru/employer/123", "https://hh.ru/employer/123", true); err != nil {
		t.Fatal(err)
	}
}

func TestParseHHSourceInput(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"sourceUrl":"https://hh.ru/employer/123","enabled":true,"revision":0}`, true},
		{`{"sourceUrl":"https://hh.ru/employer/123","enabled":false,"revision":1}`, true},
		{`{"sourceUrl":"https://hh.ru/employer/123","enabled":"true","revision":0}`, false},
		{`{"sourceUrl":"https://hh.ru/employer/123","enabled":true}`, false},
		{`{"sourceUrl":"https://hh.ru/employer/123","enabled":true,"revision":0,"owner":1}`, false},
	} {
		_, err := ParseHHSource([]byte(tc.body))
		if (err == nil) != tc.valid {
			t.Fatalf("input %s: %v", tc.body, err)
		}
	}
}
