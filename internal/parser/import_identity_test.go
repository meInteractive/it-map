package parser

import "testing"

func TestImportHHPublicationIdentity(t *testing.T) {
	cases := map[string]string{
		"https://hh.ru/vacancy/123?from=search": "hh:123",
		"https://moscow.hh.ru/vacancy/123/":     "hh:123",
		"https://HH.RU:443/vacancy/001":         "hh:001",
		"http://hh.ru/vacancy/123":              "",
		"https://hh.ru.evil.ru/vacancy/123":     "",
		"https://u@hh.ru/vacancy/123":           "",
		"https://hh.ru:444/vacancy/123":         "",
		"https://hh.ru/vacancy/abc":             "",
		"https://hh.ru/vacancy/123/extra":       "",
	}
	for raw, want := range cases {
		if got := importHHIdentity(raw); got != want {
			t.Errorf("%q: got %q want %q", raw, got, want)
		}
	}
}
