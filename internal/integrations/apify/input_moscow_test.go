package apify

import (
	"net/url"
	"testing"
)

func TestMoscowSearchScope(t *testing.T) {
	for _, source := range []string{
		"https://hh.ru/employer/780654?hhtmFrom=main&tab=VACANCIES",
		"https://spb.hh.ru/search/vacancy?employer_id=780654&area=2&area=113",
	} {
		input, err := HHInput(source, HHOptions{MoscowFilter: true})
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(input.StartURL)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if u.Host != "hh.ru" || u.Path != "/search/vacancy" || q.Get("employer_id") != "780654" || len(q["area"]) != 1 || q.Get("area") != "1" {
			t.Fatalf("incorrect bounded search %s", input.StartURL)
		}
		if !input.APIOnly {
			t.Fatal("listing must use search")
		}
	}
	legacy, err := HHInput("https://hh.ru/employer/780654", HHOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(legacy.StartURL)
	if _, ok := u.Query()["area"]; ok {
		t.Fatal("legacy paid operation identity changed")
	}
	if _, err = HHInput("https://hh.ru/vacancy/123", HHOptions{MoscowFilter: true}); err == nil {
		t.Fatal("unverified direct vacancy accepted as a Moscow listing")
	}
	if _, err = HHInput("https://hh.ru/vacancy/123", HHOptions{Detail: true, MoscowFilter: true}); err != nil {
		t.Fatal("direct details must remain available", err)
	}
}
