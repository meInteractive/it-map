package applications

import (
	"context"
	"strings"
	"testing"
)

func TestApplicationSourcesNormalize(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{`["hh","site","hh","bogus","telegram"]`, "hh,site,telegram"},
		{`["hcm","email","habr"]`, "hcm,email,habr"},
		{`["bogus"]`, "hh"}, {`null`, "hh"}, {`{}`, "hh"}, {``, "hh"},
	} {
		got := strings.Join(AllowedSources(tc.input), ",")
		if got != tc.want {
			t.Fatalf("%q: %s != %s", tc.input, got, tc.want)
		}
	}
}
func TestApplicationRevisionAndEntities(t *testing.T) {
	if got := Fingerprint("[]"); got != "4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945" {
		t.Fatal(got)
	}
	data := []byte(`{"companies":[{"name":"A","rank":1}],"agencies":[{"name":"B","rank":3}]}`)
	items, err := entityDirectory(data, map[string]bool{"B": true})
	if err != nil || len(items) != 1 || items[0].EntityType != "agency" {
		t.Fatal(items, err)
	}
	if _, err = entityDirectory([]byte(`{"companies":[]}`), nil); err == nil {
		t.Fatal("malformed catalog accepted")
	}
}
func TestApplicationsUnavailable(t *testing.T) {
	r := Repository{}
	if _, err := r.List(context.Background(), "Demo", "", nil); err == nil {
		t.Fatal("accepted nil DB")
	}
}
