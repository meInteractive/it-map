package apify

import (
	"net/url"
	"reflect"
	"testing"
)

func TestHHInputProfessionalRoles(t *testing.T) {
	input, err := HHInput("https://hh.ru/search/vacancy?employer_id=780654&text=manager&professional_role=9&area=2", HHOptions{MoscowFilter: true, ProfessionalRoles: []string{"36", "73", "36"}})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(input.StartURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if !reflect.DeepEqual(q["professional_role"], []string{"36", "73"}) || q.Has("text") || q.Get("employer_id") != "780654" || q.Get("area") != "1" {
		t.Fatalf("unexpected search parameters: %v", q)
	}
	for _, roles := range [][]string{{}, {"0"}, {"01"}, {"1&text=x"}, {""}} {
		if _, err := HHInput("https://hh.ru/employer/780654", HHOptions{ProfessionalRoles: roles}); err == nil {
			t.Errorf("accepted invalid roles %q", roles)
		}
	}
	query := "NAME:(manager)"
	if _, err := HHInput("https://hh.ru/employer/780654", HHOptions{ProfessionalRoles: []string{"36"}, TitleQuery: &query}); err == nil {
		t.Fatal("accepted a title filter with professional roles")
	}
	if _, err := HHInput("https://hh.ru/vacancy/123", HHOptions{ProfessionalRoles: []string{"36"}}); err == nil {
		t.Fatal("accepted roles on a vacancy URL")
	}
}
